"use client";

import { useState, useMemo } from "react";
import {
  ChevronRight,
  ChevronDown,
  FileText,
  Folder,
  FolderOpen,
} from "lucide-react";
import { cn } from "@/lib/utils";
import type { FileInfo } from "@squat-collective/rat-client";

type TreeNode = {
  name: string;
  path: string;
  type: "file" | "dir";
  children: TreeNode[];
  fileInfo?: FileInfo;
};

function buildTree(files: FileInfo[]): TreeNode[] {
  const root: TreeNode[] = [];

  for (const file of files) {
    const parts = file.path.split("/").filter(Boolean);
    let current = root;

    for (let i = 0; i < parts.length; i++) {
      const part = parts[i];
      const isLast = i === parts.length - 1;
      const existing = current.find((n) => n.name === part);

      if (existing) {
        if (isLast) {
          existing.fileInfo = file;
        }
        current = existing.children;
      } else {
        const node: TreeNode = {
          name: part,
          path: parts.slice(0, i + 1).join("/"),
          type: isLast ? "file" : "dir",
          children: [],
          fileInfo: isLast ? file : undefined,
        };
        current.push(node);
        current = node.children;
      }
    }
  }

  // Sort: dirs first, then alphabetical
  const sortNodes = (nodes: TreeNode[]) => {
    nodes.sort((a, b) => {
      if (a.type !== b.type) return a.type === "dir" ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    nodes.forEach((n) => sortNodes(n.children));
  };
  sortNodes(root);

  return root;
}

const LAYER_COLORS: Record<string, string> = {
  bronze: "text-orange-500",
  silver: "text-zinc-400",
  gold: "text-yellow-500",
};

type FileTreeProps = {
  files: FileInfo[];
  onSelect: (path: string) => void;
  onContextMenu?: (path: string) => void;
  selectedPath?: string | null;
};

export function FileTree({ files, onSelect, onContextMenu, selectedPath }: FileTreeProps) {
  const tree = useMemo(() => buildTree(files), [files]);

  return (
    <ul role="tree" tabIndex={0} className="text-[11px] font-mono overflow-y-auto list-none m-0 p-0">
      {tree.map((node) => (
        <FileTreeNode
          key={node.path}
          node={node}
          depth={0}
          onSelect={onSelect}
          onContextMenu={onContextMenu}
          selectedPath={selectedPath}
        />
      ))}
    </ul>
  );
}

function FileTreeNode({
  node,
  depth,
  onSelect,
  onContextMenu,
  selectedPath,
}: {
  node: TreeNode;
  depth: number;
  onSelect: (path: string) => void;
  onContextMenu?: (path: string) => void;
  selectedPath?: string | null;
}) {
  const [expanded, setExpanded] = useState(depth < 2);
  const layerColor =
    LAYER_COLORS[node.name] || (depth === 0 ? "text-primary" : "");

  if (node.type === "dir") {
    return (
      <li role="treeitem" aria-expanded={expanded} tabIndex={-1}>
        <button
          onClick={() => setExpanded(!expanded)}
          onContextMenu={() => onContextMenu?.(node.path)}
          className={cn(
            "flex items-center gap-1 w-full px-2 py-0.5 hover:bg-accent/50 text-left",
            layerColor,
          )}
          style={{ paddingLeft: `${depth * 12 + 8}px` }}
        >
          {expanded ? (
            <>
              <ChevronDown className="h-3 w-3 shrink-0" />
              <FolderOpen className="h-3 w-3 shrink-0" />
            </>
          ) : (
            <>
              <ChevronRight className="h-3 w-3 shrink-0" />
              <Folder className="h-3 w-3 shrink-0" />
            </>
          )}
          <span className="truncate">{node.name}</span>
        </button>
        {expanded && (
          <ul role="group" className="list-none m-0 p-0">
            {node.children.map((child) => (
              <FileTreeNode
                key={child.path}
                node={child}
                depth={depth + 1}
                onSelect={onSelect}
                onContextMenu={onContextMenu}
                selectedPath={selectedPath}
              />
            ))}
          </ul>
        )}
      </li>
    );
  }

  const isSelected = selectedPath === node.path;

  return (
    <li role="treeitem" aria-selected={isSelected} tabIndex={-1}>
      <button
        onClick={() => onSelect(node.path)}
        onContextMenu={() => onContextMenu?.(node.path)}
        className={cn(
          "flex items-center gap-1 w-full px-2 py-0.5 hover:bg-accent/50 text-left",
          isSelected &&
            "bg-primary/10 text-primary border-l-2 border-primary",
        )}
        style={{ paddingLeft: `${depth * 12 + 8}px` }}
      >
        <FileText className="h-3 w-3 shrink-0 text-muted-foreground" />
        <span className="truncate">{node.name}</span>
      </button>
    </li>
  );
}
