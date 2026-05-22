"use client";

import { forwardRef } from "react";
import { SqlEditor, type SqlEditorHandle } from "@/components/sql-editor";
import { useQuerySchema } from "@/hooks/use-api";

type SqlEditorPluginProps = {
  value: string;
  onChange: (value: string) => void;
  onExecute?: () => void;
};

/**
 * SqlEditorWithSchema is the portal's CodeMirror SQL editor wired to the live
 * catalog schema, so plugin bundles get schema-aware autocomplete (table and
 * column names) for free — without shipping their own editor.
 */
export const SqlEditorWithSchema = forwardRef<SqlEditorHandle, SqlEditorPluginProps>(
  function SqlEditorWithSchema(props, ref) {
    const { data: schema } = useQuerySchema();
    return <SqlEditor {...props} ref={ref} schema={schema} />;
  },
);

/**
 * pluginUIKit is exposed to plugin bundles on window.__RAT_UI so they can reuse
 * portal UI — currently the SQL editor — rather than bundling their own.
 */
export const pluginUIKit = {
  SqlEditor,
  SqlEditorWithSchema,
};
