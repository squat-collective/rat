"use client";

import {
  useEffect,
  useRef,
  useImperativeHandle,
  forwardRef,
  useMemo,
} from "react";
import { EditorView, keymap } from "@codemirror/view";
import { EditorState } from "@codemirror/state";
import { sql } from "@codemirror/lang-sql";
import { oneDark } from "@codemirror/theme-one-dark";
import { basicSetup } from "codemirror";
import {
  type SchemaData,
  buildCmSchema,
  aliasColumnCompletion,
  columnLinter,
} from "@/lib/sql-schema";

type SqlEditorProps = {
  value: string;
  onChange: (value: string) => void;
  onExecute?: () => void;
  schema?: SchemaData;
};

export type SqlEditorHandle = {
  insertAtCursor: (text: string) => void;
};

export const SqlEditor = forwardRef<SqlEditorHandle, SqlEditorProps>(
  function SqlEditor({ value, onChange, onExecute, schema: schemaProp }, ref) {
    const containerRef = useRef<HTMLDivElement>(null);
    const viewRef = useRef<EditorView | null>(null);

    // Keep stable refs for callbacks so the memoized extensions don't churn
    const onChangeRef = useRef(onChange);
    onChangeRef.current = onChange;
    const onExecuteRef = useRef(onExecute);
    onExecuteRef.current = onExecute;

    const cmSchema = useMemo(
      () => (schemaProp ? buildCmSchema(schemaProp) : undefined),
      [schemaProp],
    );

    useImperativeHandle(ref, () => ({
      insertAtCursor(text: string) {
        const view = viewRef.current;
        if (!view) return;
        const { from } = view.state.selection.main;
        view.dispatch({
          changes: { from, to: from, insert: text },
          selection: { anchor: from + text.length },
        });
        view.focus();
      },
    }));

    const extensions = useMemo(() => {
      const executeKeymap = keymap.of([
        {
          key: "Ctrl-Enter",
          run: () => {
            onExecuteRef.current?.();
            return true;
          },
        },
        {
          key: "Mod-Enter",
          run: () => {
            onExecuteRef.current?.();
            return true;
          },
        },
      ]);

      const sqlExt = cmSchema
        ? sql({
            schema: cmSchema as NonNullable<Parameters<typeof sql>[0]>["schema"],
            upperCaseKeywords: true,
          })
        : sql();

      const aliasSource = schemaProp
        ? aliasColumnCompletion(schemaProp, "plain")
        : null;
      const colLint = schemaProp
        ? columnLinter(schemaProp, "plain")
        : null;

      return [
        basicSetup,
        sqlExt,
        oneDark,
        executeKeymap,
        ...(aliasSource
          ? [EditorState.languageData.of(() => [{ autocomplete: aliasSource }])]
          : []),
        ...(colLint ? [colLint] : []),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            onChangeRef.current(update.state.doc.toString());
          }
        }),
        EditorView.theme({
          "&": { height: "200px" },
          ".cm-scroller": { overflow: "auto" },
        }),
      ];
    }, [cmSchema, schemaProp]);

    useEffect(() => {
      if (!containerRef.current) return;

      const state = EditorState.create({
        doc: value,
        extensions,
      });

      const view = new EditorView({
        state,
        parent: containerRef.current,
      });

      viewRef.current = view;

      return () => {
        view.destroy();
      };
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [extensions]);

    return (
      <div
        ref={containerRef}
        className="overflow-hidden border border-border/50"
      />
    );
  },
);
