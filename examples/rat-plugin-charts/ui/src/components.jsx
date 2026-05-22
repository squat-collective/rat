// Small, self-contained UI primitives for the charts plugin. The portal does
// not expose a component library to plugin bundles, so we ship our own — kept
// minimal and themed with the portal's CSS variables (with dark fallbacks).

import React from "react";

export const C = {
  border: "var(--border, #2a2a2a)",
  fg: "var(--foreground, #e5e5e5)",
  muted: "var(--muted-foreground, #8a8a8a)",
  primary: "var(--primary, #4ade80)",
  primaryFg: "var(--primary-foreground, #0a0a0a)",
  danger: "var(--destructive, #f87171)",
  card: "var(--card, #141414)",
  bg: "var(--background, #0a0a0a)",
  popover: "var(--popover, #1a1a1a)",
};

// ── Buttons ───────────────────────────────────────────────────────

export function Button(props) {
  const { variant, onClick, disabled, children, style, type, title } = props;
  const base = {
    padding: "0.4rem 0.8rem",
    fontSize: "0.78rem",
    fontWeight: 600,
    border: "1px solid " + C.border,
    background: "transparent",
    color: C.fg,
    cursor: disabled ? "default" : "pointer",
    opacity: disabled ? 0.45 : 1,
    fontFamily: "inherit",
    lineHeight: 1.25,
    whiteSpace: "nowrap",
  };
  if (variant === "primary") {
    base.background = C.primary;
    base.color = C.primaryFg;
    base.borderColor = C.primary;
  } else if (variant === "danger") {
    base.color = C.danger;
    base.borderColor = C.danger;
  } else if (variant === "ghost") {
    base.borderColor = "transparent";
  }
  return (
    <button
      type={type || "button"}
      title={title}
      onClick={onClick}
      disabled={disabled}
      style={{ ...base, ...style }}
    >
      {children}
    </button>
  );
}

// ── Cards ─────────────────────────────────────────────────────────

export function Card(props) {
  const { children, style, onClick } = props;
  return (
    <div
      className="brutal-card"
      onClick={onClick}
      style={{
        border: "1px solid " + C.border,
        background: C.card,
        padding: "1rem",
        ...(onClick ? { cursor: "pointer" } : null),
        ...style,
      }}
    >
      {children}
    </div>
  );
}

export function Badge(props) {
  return (
    <span
      style={{
        fontSize: "0.62rem",
        fontWeight: 700,
        letterSpacing: "0.06em",
        textTransform: "uppercase",
        border: "1px solid " + C.border,
        padding: "0.1rem 0.4rem",
        color: C.muted,
        ...props.style,
      }}
    >
      {props.children}
    </span>
  );
}

// ── Form fields ───────────────────────────────────────────────────

const fieldInput = {
  width: "100%",
  padding: "0.45rem 0.55rem",
  fontSize: "0.82rem",
  fontFamily: "inherit",
  background: C.bg,
  color: C.fg,
  border: "1px solid " + C.border,
};

export function Field(props) {
  return (
    <label style={{ display: "block", marginBottom: "0.75rem" }}>
      <div
        style={{
          fontSize: "0.66rem",
          fontWeight: 700,
          letterSpacing: "0.06em",
          textTransform: "uppercase",
          color: C.muted,
          marginBottom: "0.25rem",
        }}
      >
        {props.label}
      </div>
      {props.children}
      {props.hint ? (
        <div style={{ fontSize: "0.68rem", color: C.muted, marginTop: "0.2rem" }}>
          {props.hint}
        </div>
      ) : null}
    </label>
  );
}

export function TextInput(props) {
  return (
    <input
      type="text"
      value={props.value}
      placeholder={props.placeholder}
      onChange={(e) => props.onChange(e.target.value)}
      style={{ ...fieldInput, ...props.style }}
    />
  );
}

export function TextArea(props) {
  return (
    <textarea
      value={props.value}
      placeholder={props.placeholder}
      rows={props.rows || 4}
      onChange={(e) => props.onChange(e.target.value)}
      style={{ ...fieldInput, resize: "vertical", fontFamily: "monospace", ...props.style }}
    />
  );
}

export function Select(props) {
  return (
    <select
      value={props.value}
      onChange={(e) => props.onChange(e.target.value)}
      style={{ ...fieldInput, ...props.style }}
    >
      {props.children}
    </select>
  );
}

// ── Modal ─────────────────────────────────────────────────────────

export function Modal(props) {
  return (
    <div
      onClick={props.onClose}
      style={{
        position: "fixed",
        inset: 0,
        background: "rgba(0,0,0,0.65)",
        zIndex: 60,
        display: "flex",
        alignItems: "flex-start",
        justifyContent: "center",
        padding: "3rem 1rem",
        overflowY: "auto",
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          background: C.bg,
          border: "1px solid " + C.border,
          width: "100%",
          maxWidth: props.wide ? "54rem" : "34rem",
          padding: "1.25rem",
        }}
      >
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            marginBottom: "1rem",
          }}
        >
          <h2 style={{ fontWeight: 700, fontSize: "1rem" }}>{props.title}</h2>
          <Button variant="ghost" onClick={props.onClose} style={{ padding: "0.2rem 0.5rem" }}>
            ✕
          </Button>
        </div>
        {props.children}
      </div>
    </div>
  );
}

// ── Status helpers ────────────────────────────────────────────────

export function Loading(props) {
  return (
    <div style={{ color: C.muted, fontSize: "0.82rem", padding: "1rem" }}>
      {props.text || "Loading…"}
    </div>
  );
}

export function ErrorText(props) {
  return (
    <div
      style={{
        color: C.danger,
        fontSize: "0.78rem",
        border: "1px solid " + C.danger,
        padding: "0.5rem 0.65rem",
        background: "rgba(248,113,113,0.06)",
      }}
    >
      {props.children}
    </div>
  );
}

export function EmptyState(props) {
  return (
    <div
      style={{
        border: "1px dashed " + C.border,
        padding: "2rem 1rem",
        textAlign: "center",
        color: C.muted,
        fontSize: "0.85rem",
      }}
    >
      <div style={{ fontSize: "1.6rem", marginBottom: "0.4rem" }}>{props.icon || "○"}</div>
      <div>{props.children}</div>
    </div>
  );
}

// ── Minimal markdown (for report text blocks) ─────────────────────

const codeStyle = {
  fontFamily: "monospace",
  fontSize: "0.8rem",
  background: "rgba(255,255,255,0.08)",
  padding: "0.05rem 0.25rem",
};
const preStyle = {
  fontFamily: "monospace",
  fontSize: "0.78rem",
  whiteSpace: "pre-wrap",
  background: "rgba(255,255,255,0.06)",
  padding: "0.6rem",
  overflowX: "auto",
  margin: "0.5rem 0",
};

function renderInline(text, keyBase) {
  const parts = String(text).split(/(`[^`]+`|\*\*[^*]+\*\*)/g);
  return parts.map((p, i) => {
    if (p.length > 1 && p[0] === "`" && p[p.length - 1] === "`") {
      return (
        <code key={keyBase + "-" + i} style={codeStyle}>
          {p.slice(1, -1)}
        </code>
      );
    }
    if (p.length > 4 && p.slice(0, 2) === "**" && p.slice(-2) === "**") {
      return <strong key={keyBase + "-" + i}>{p.slice(2, -2)}</strong>;
    }
    return p;
  });
}

export function Markdown(props) {
  const lines = String(props.text || "").split("\n");
  const blocks = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const heading = /^(#{1,3})\s+(.*)$/.exec(line);
    if (line.trim().slice(0, 3) === "```") {
      const code = [];
      i++;
      while (i < lines.length && lines[i].trim().slice(0, 3) !== "```") {
        code.push(lines[i]);
        i++;
      }
      i++;
      blocks.push(
        <pre key={blocks.length} style={preStyle}>
          {code.join("\n")}
        </pre>,
      );
    } else if (heading) {
      const sizes = { 1: "1.2rem", 2: "1rem", 3: "0.88rem" };
      blocks.push(
        <div
          key={blocks.length}
          style={{ fontWeight: 700, fontSize: sizes[heading[1].length], margin: "0.7rem 0 0.35rem" }}
        >
          {renderInline(heading[2], "h" + blocks.length)}
        </div>,
      );
      i++;
    } else if (/^\s*[-*]\s+/.test(line)) {
      const items = [];
      while (i < lines.length && /^\s*[-*]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*[-*]\s+/, ""));
        i++;
      }
      blocks.push(
        <ul key={blocks.length} style={{ margin: "0.4rem 0", paddingLeft: "1.2rem" }}>
          {items.map((it, j) => (
            <li key={j}>{renderInline(it, "l" + blocks.length + "-" + j)}</li>
          ))}
        </ul>,
      );
    } else if (line.trim() === "") {
      i++;
    } else {
      const para = [];
      while (
        i < lines.length &&
        lines[i].trim() !== "" &&
        lines[i].trim().slice(0, 3) !== "```" &&
        !/^\s*[-*]\s+/.test(lines[i]) &&
        !/^(#{1,3})\s+/.test(lines[i])
      ) {
        para.push(lines[i]);
        i++;
      }
      blocks.push(
        <div
          key={blocks.length}
          style={{ margin: "0.4rem 0", whiteSpace: "pre-wrap", lineHeight: 1.6 }}
        >
          {renderInline(para.join("\n"), "p" + blocks.length)}
        </div>,
      );
    }
  }
  return <div>{blocks}</div>;
}
