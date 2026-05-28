"use client";

import { useEffect, useRef } from "react";
import { EditorView, keymap } from "@codemirror/view";
import { EditorState, type Extension } from "@codemirror/state";
import { basicSetup } from "codemirror";
import { oneDark } from "@codemirror/theme-one-dark";
import { indentWithTab } from "@codemirror/commands";
import { sql } from "@codemirror/lang-sql";
import { python } from "@codemirror/lang-python";
import { yaml } from "@codemirror/lang-yaml";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { javascript } from "@codemirror/lang-javascript";
import { html } from "@codemirror/lang-html";
import { css } from "@codemirror/lang-css";
import { xml } from "@codemirror/lang-xml";
import { rust } from "@codemirror/lang-rust";
import { go } from "@codemirror/lang-go";

// Languages a plugin config field can request via its JSON Schema `format`.
// Each maps to a CodeMirror language extension — add more by installing the
// matching @codemirror/lang-* package and adding an entry here.
const LANGUAGES: Record<string, () => Extension> = {
  sql: () => sql(),
  python: () => python(),
  yaml: () => yaml(),
  json: () => json(),
  markdown: () => markdown(),
  javascript: () => javascript(),
  jsx: () => javascript({ jsx: true }),
  typescript: () => javascript({ typescript: true }),
  tsx: () => javascript({ typescript: true, jsx: true }),
  html: () => html(),
  css: () => css(),
  xml: () => xml(),
  rust: () => rust(),
  go: () => go(),
};

/** `format` values that ConfigCodeField renders as a code editor. */
export const CODE_FORMATS = Object.keys(LANGUAGES);

interface ConfigCodeFieldProps {
  value: string;
  language: string;
  onChange: (value: string) => void;
}

/**
 * A lightweight, language-aware editor for schema-driven plugin config fields.
 * Unlike the pipeline CodeEditor it carries no pipeline-specific completions or
 * keymaps — it is a plain code editor a config form can drop in for any field
 * whose schema declares a code `format` (e.g. "sql").
 */
export function ConfigCodeField({
  value,
  language,
  onChange,
}: ConfigCodeFieldProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  // Create the editor once per language. External value changes are synced by
  // the effect below so typing is never interrupted.
  useEffect(() => {
    if (!containerRef.current) return;

    const langExt = LANGUAGES[language]?.() ?? [];
    const state = EditorState.create({
      doc: value ?? "",
      extensions: [
        basicSetup,
        langExt,
        oneDark,
        keymap.of([indentWithTab]),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            onChangeRef.current(update.state.doc.toString());
          }
        }),
        EditorView.theme({
          "&": { fontSize: "12px" },
          ".cm-scroller": { maxHeight: "280px", overflow: "auto" },
        }),
      ],
    });

    const view = new EditorView({ state, parent: containerRef.current });
    viewRef.current = view;
    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [language]);

  // Sync external value changes (e.g. a config reload) into the editor.
  useEffect(() => {
    const view = viewRef.current;
    if (view && value !== view.state.doc.toString()) {
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: value ?? "" },
      });
    }
  }, [value]);

  return <div ref={containerRef} className="border border-border" />;
}
