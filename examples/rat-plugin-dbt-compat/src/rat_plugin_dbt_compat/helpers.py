"""dbt-compatible Jinja helper functions.

Provides env_var(), var(), and generate_schema_name() for use in RAT SQL templates,
matching dbt's familiar template API.
"""

from __future__ import annotations

import os


class EnvVarHelper:
    """Read environment variables in SQL templates.

    Usage in templates:
        {{ env_var('DB_HOST') }}
        {{ env_var('DB_PORT', '5432') }}
    """

    @property
    def name(self) -> str:
        return "env_var"

    def __call__(self, *args: object, **kwargs: object) -> object:
        if not args:
            raise TypeError("env_var() requires at least 1 argument (variable name)")

        var_name = str(args[0])
        default = args[1] if len(args) > 1 else None

        value = os.environ.get(var_name)
        if value is not None:
            return value

        if default is not None:
            return default

        raise ValueError(
            f"env_var('{var_name}'): environment variable not found and no default provided"
        )


class VarHelper:
    """Placeholder for dbt-style project variables.

    In dbt, var() reads from dbt_project.yml variables. In RAT, this is a
    forward-compatible placeholder that returns the key name. Future versions
    may read from rat.yaml config variables.

    Usage in templates:
        {{ var('batch_size') }}
    """

    @property
    def name(self) -> str:
        return "var"

    def __call__(self, *args: object, **kwargs: object) -> object:
        if not args:
            raise TypeError("var() requires at least 1 argument (variable name)")
        return str(args[0])


class GenerateSchemaNameHelper:
    """dbt-style schema name generation.

    In dbt, generate_schema_name() controls which schema a model is built in.
    RAT doesn't use database schemas (it uses namespace.layer.name), so this
    simply returns the argument as-is for compatibility.

    Usage in templates:
        {{ generate_schema_name('custom') }}
    """

    @property
    def name(self) -> str:
        return "generate_schema_name"

    def __call__(self, *args: object, **kwargs: object) -> object:
        if not args:
            raise TypeError("generate_schema_name() requires at least 1 argument")
        return str(args[0])
