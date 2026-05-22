// The dashboard grid — react-grid-layout drives drag, drop and resize. Each
// grid cell holds one ComponentCard; in edit mode a card gets a drag handle
// and edit/delete controls.

import React from "react";
import RGL, { WidthProvider } from "react-grid-layout";
import rglCss from "react-grid-layout/css/styles.css";
import rrCss from "react-resizable/css/styles.css";
import { ComponentBody } from "./cmp.jsx";
import { C } from "./components.jsx";

const Grid = WidthProvider(RGL);
const ROW_H = 40;

// react-grid-layout needs its CSS; inject it once, plus a couple of overrides.
let cssInjected = false;
function ensureGridCSS() {
  if (cssInjected || typeof document === "undefined") return;
  cssInjected = true;
  const style = document.createElement("style");
  style.textContent =
    rglCss +
    "\n" +
    rrCss +
    "\n.react-grid-item.react-grid-placeholder{background:" +
    C.primary +
    ";opacity:0.18;border-radius:0;}" +
    "\n.rat-cmp-drag{cursor:move;}";
  document.head.appendChild(style);
}

// Chart, metric and AI components render inside a bordered card; headings and
// text float free so they read as page content.
const carded = { chart: true, metric: true, ai: true };
const TYPE_LABEL = {
  chart: "Chart",
  heading: "Heading",
  markdown: "Text",
  metric: "Metric",
  ai: "AI analysis",
};

const hdrBtn = {
  fontSize: "0.62rem",
  fontWeight: 700,
  padding: "0.25rem 0.55rem",
  border: "none",
  borderLeft: "1px solid " + C.border,
  background: "transparent",
  color: C.fg,
  cursor: "pointer",
  fontFamily: "inherit",
};

function ComponentCard(props) {
  const { component, editing } = props;
  const isCard = !!carded[component.type];
  return (
    <div
      style={{
        height: "100%",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        border: "1px solid " + (isCard ? C.border : "transparent"),
        background: isCard ? C.card : "transparent",
      }}
    >
      {editing ? (
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            borderBottom: "1px solid " + C.border,
            background: C.surface,
          }}
        >
          <span
            className="rat-cmp-drag"
            style={{
              flex: 1,
              padding: "0.25rem 0.55rem",
              fontSize: "0.62rem",
              fontWeight: 700,
              letterSpacing: "0.06em",
              textTransform: "uppercase",
              color: C.muted,
              userSelect: "none",
            }}
          >
            ⠿ {TYPE_LABEL[component.type] || component.type}
          </span>
          <button onClick={() => props.onEdit(component)} style={hdrBtn}>
            edit
          </button>
          <button
            onClick={() => props.onDelete(component.id)}
            style={{ ...hdrBtn, color: C.danger }}
          >
            ✕
          </button>
        </div>
      ) : null}
      <div
        style={{
          flex: 1,
          minHeight: 0,
          padding: component.type === "heading" ? "0 0.3rem" : "0.6rem 0.7rem",
        }}
      >
        <ComponentBody
          component={component}
          components={props.components}
          refreshKey={props.refreshKey}
          onUpdateComponent={props.onUpdateComponent}
        />
      </div>
    </div>
  );
}

// DashboardGrid renders the components on a 12-column react-grid-layout grid.
// onLayoutChange receives RGL's layout array after a drag or resize.
export function DashboardGrid(props) {
  const { components, editing } = props;
  ensureGridCSS();

  const layout = components.map((c) => ({
    i: c.id,
    x: c.layout.x,
    y: c.layout.y,
    w: c.layout.w,
    h: c.layout.h,
    minW: 2,
    minH: 2,
  }));

  return (
    <Grid
      className="layout"
      layout={layout}
      cols={12}
      rowHeight={ROW_H}
      margin={[14, 14]}
      isDraggable={editing}
      isResizable={editing}
      draggableHandle=".rat-cmp-drag"
      compactType="vertical"
      onDragStop={(l) => props.onLayoutChange(l)}
      onResizeStop={(l) => props.onLayoutChange(l)}
    >
      {components.map((c) => (
        <div key={c.id}>
          <ComponentCard
            component={c}
            components={components}
            editing={editing}
            refreshKey={props.refreshKey}
            onEdit={props.onEdit}
            onDelete={props.onDelete}
            onUpdateComponent={props.onUpdateComponent}
          />
        </div>
      ))}
    </Grid>
  );
}
