"""Dependency DAG validation — build and validate pipeline dependency graphs.

Extracts ref() calls from pipeline SQL files to build a directed acyclic graph
(DAG). Provides cycle detection to prevent infinite dependency loops.

Usage:
    from rat_runner.dag import build_dag, detect_cycles, validate_dag

    dag = build_dag(pipelines)  # dict of pipeline_key -> set of dependency keys
    cycles = detect_cycles(dag)
    if cycles:
        raise ValueError(f"Circular dependencies found: {cycles}")
"""

from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import NamedTuple

from rat_runner.templating import extract_dependencies

logger = logging.getLogger(__name__)


class PipelineRef(NamedTuple):
    """Unique identifier for a pipeline in the DAG."""

    namespace: str
    layer: str
    name: str


@dataclass(frozen=True)
class PipelineSource:
    """A pipeline with its SQL source for DAG analysis."""

    namespace: str
    layer: str
    name: str
    sql: str


def build_dag(
    pipelines: list[PipelineSource],
    default_namespace: str = "default",
) -> dict[PipelineRef, set[PipelineRef]]:
    """Build a dependency DAG from pipeline SQL sources.

    Extracts ref() calls from each pipeline's SQL and resolves them to
    PipelineRef keys. Returns an adjacency list where each key maps to its
    set of dependencies (upstream pipelines).

    ref() resolution:
    - 2-part "layer.name" -> (default_namespace, layer, name)
    - 3-part "ns.layer.name" -> (ns, layer, name)
    """
    dag: dict[PipelineRef, set[PipelineRef]] = {}

    for p in pipelines:
        key = PipelineRef(p.namespace, p.layer, p.name)
        deps: set[PipelineRef] = set()

        refs = extract_dependencies(p.sql)
        for ref in refs:
            parts = ref.split(".", 2)
            if len(parts) == 2:
                dep = PipelineRef(default_namespace, parts[0], parts[1])
            elif len(parts) == 3:
                dep = PipelineRef(parts[0], parts[1], parts[2])
            else:
                logger.warning("Invalid ref '%s' in pipeline %s, skipping", ref, key)
                continue
            deps.add(dep)

        dag[key] = deps

    return dag


def detect_cycles(dag: dict[PipelineRef, set[PipelineRef]]) -> list[list[PipelineRef]]:
    """Detect all cycles in the dependency DAG using iterative DFS.

    Returns a list of cycles, where each cycle is a list of PipelineRef nodes
    forming the loop. Returns an empty list if the DAG is acyclic.

    Uses the standard three-color (white/gray/black) algorithm:
    - White: unvisited
    - Gray: currently in the DFS stack (being processed)
    - Black: fully processed (all descendants visited)

    A cycle exists when we encounter a gray node during DFS.
    """
    white, gray, black = 0, 1, 2
    color: dict[PipelineRef, int] = {node: white for node in dag}
    parent: dict[PipelineRef, PipelineRef | None] = {}
    cycles: list[list[PipelineRef]] = []

    def _dfs(start: PipelineRef) -> None:
        stack: list[tuple[PipelineRef, bool]] = [(start, False)]

        while stack:
            node, backtrack = stack.pop()

            if backtrack:
                color[node] = black
                continue

            if color[node] == black:
                continue

            if color[node] == gray:
                # Already processing — skip (will be marked black by backtrack entry)
                continue

            color[node] = gray
            # Push backtrack marker
            stack.append((node, True))

            for dep in dag.get(node, set()):
                if dep not in color:
                    # Dependency references a pipeline not in the DAG — external, skip.
                    continue

                if color[dep] == gray:
                    # Found a cycle — reconstruct it.
                    cycle = _reconstruct_cycle(node, dep, parent)
                    if cycle:
                        cycles.append(cycle)
                elif color[dep] == white:
                    parent[dep] = node
                    stack.append((dep, False))

    for node in dag:
        if color[node] == white:
            parent[node] = None
            _dfs(node)

    return cycles


def _reconstruct_cycle(
    current: PipelineRef,
    target: PipelineRef,
    parent: dict[PipelineRef, PipelineRef | None],
) -> list[PipelineRef]:
    """Reconstruct a cycle path from parent pointers.

    Walks backward from current to target via parent links.
    Returns the cycle as [target, ..., current, target].
    """
    path = [current]
    node = current
    seen = {current}
    while node != target:
        p = parent.get(node)
        if p is None or p in seen:
            # Can't fully reconstruct — return what we have.
            break
        path.append(p)
        seen.add(p)
        node = p
    path.append(target)
    path.reverse()
    return path


def validate_dag(
    pipelines: list[PipelineSource],
    default_namespace: str = "default",
) -> list[str]:
    """Validate the pipeline dependency DAG and return error messages.

    Returns an empty list if the DAG is valid (no cycles).
    Returns human-readable error messages for each detected cycle.
    """
    dag = build_dag(pipelines, default_namespace)
    cycles = detect_cycles(dag)

    errors: list[str] = []
    for cycle in cycles:
        path_str = " -> ".join(f"{n.namespace}.{n.layer}.{n.name}" for n in cycle)
        errors.append(f"Circular dependency detected: {path_str}")

    return errors
