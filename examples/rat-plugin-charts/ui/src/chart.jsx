// The chart renderer — real Recharts, themed to the RAT portal and driven by a
// rich ChartOptions object so the chart builder and the AI assistant can both
// produce richly-styled charts. ChartView draws from explicit rows; LiveChart
// fetches the rows by re-running the chart's saved query.

import React from "react";
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  LineChart,
  Line,
  AreaChart,
  Area,
  PieChart,
  Pie,
  Cell,
  RadarChart,
  Radar,
  PolarGrid,
  PolarAngleAxis,
  PolarRadiusAxis,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  LabelList,
} from "recharts";
import { api } from "./api.js";
import { C, ErrorText } from "./components.jsx";

// Named colour palettes. seriesColors() resolves a chart's series colours:
// explicit options.colors win, else the chosen palette (default "rat").
export const PALETTES = {
  rat: ["#4ade80", "#22d3ee", "#a78bfa", "#fbbf24", "#f472b6", "#60a5fa"],
  vivid: ["#6366f1", "#ec4899", "#f59e0b", "#10b981", "#ef4444", "#8b5cf6"],
  ocean: ["#38bdf8", "#22d3ee", "#2dd4bf", "#60a5fa", "#818cf8", "#0ea5e9"],
  sunset: ["#fb7185", "#fb923c", "#fbbf24", "#f472b6", "#f87171", "#facc15"],
  mono: ["#e5e5e5", "#a3a3a3", "#737373", "#525252", "#404040"],
};
export const PALETTE_NAMES = Object.keys(PALETTES);

export function seriesColors(options, n) {
  const opts = options || {};
  const explicit = opts.colors || [];
  const pal = PALETTES[opts.palette] || PALETTES.rat;
  const out = [];
  for (let i = 0; i < n; i++) out.push(explicit[i] || pal[i % pal.length]);
  return out;
}

const chartMargin = { top: 14, right: 16, bottom: 4, left: 0 };
const axisProps = { tick: { fontSize: 11, fill: "#9a9a9a" }, tickLine: false };
const tooltipProps = {
  contentStyle: {
    background: C.surface,
    border: "1px solid " + C.border,
    borderRadius: 0,
    fontSize: "0.75rem",
    color: C.fg,
  },
  labelStyle: { color: C.muted },
  itemStyle: { color: C.fg },
  cursor: { fill: "rgba(255,255,255,0.06)" },
};
const curveTypes = { smooth: "monotone", linear: "linear", step: "step" };
const labelProps = { fill: "#aaa", fontSize: 10 };

function toNum(v) {
  if (typeof v === "number") return v;
  if (v === null || v === undefined || v === "") return 0;
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

function centered(height, text) {
  return (
    <div
      style={{
        height: height,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        color: C.muted,
        fontSize: "0.78rem",
      }}
    >
      {text}
    </div>
  );
}

// ChartView draws one chart from an explicit { chart, rows } pair. The chart's
// `options` (a ChartOptions object) controls colours, type-specific styling,
// grid/legend/labels, etc.
export function ChartView(props) {
  const chart = props.chart || {};
  const opts = chart.options || {};
  const height = props.height || 260;
  const rows = props.rows || [];
  const x = chart.x_column;
  const ys = chart.y_columns && chart.y_columns.length ? chart.y_columns : [];
  const type = chart.type || "bar";

  if (!x || !ys.length) {
    return <ErrorText>chart is missing its x / y columns</ErrorText>;
  }
  if (!rows.length) {
    return centered(height, "No rows returned");
  }

  const data = rows.map((r) => {
    const o = {};
    o[x] = r[x];
    ys.forEach((y) => {
      o[y] = toNum(r[y]);
    });
    return o;
  });

  const colors = seriesColors(opts, Math.max(ys.length, type === "pie" ? data.length : 0));
  const curve = curveTypes[opts.curve] || "monotone";
  const showGrid = !opts.hide_grid;
  const showLegend = !opts.hide_legend;
  const stackId = opts.stacked ? "stack" : undefined;
  const grid = showGrid ? (
    <CartesianGrid vertical={false} strokeDasharray="3 3" stroke="#2c2c2c" />
  ) : null;
  const multiLegend = showLegend && ys.length > 1
    ? <Legend wrapperStyle={{ fontSize: "0.72rem" }} />
    : null;

  let inner;

  if (type === "pie") {
    const outer = Math.max(54, height * 0.36);
    const innerR = opts.inner_radius ? (opts.inner_radius / 100) * outer : 0;
    inner = (
      <PieChart>
        <Tooltip {...tooltipProps} />
        {showLegend ? <Legend wrapperStyle={{ fontSize: "0.72rem" }} /> : null}
        <Pie
          data={data}
          dataKey={ys[0]}
          nameKey={x}
          cx="50%"
          cy="50%"
          innerRadius={innerR}
          outerRadius={outer}
          stroke={C.bg}
          isAnimationActive={false}
          label={(e) => (opts.show_labels ? e.name + ": " + e.value : e.name)}
        >
          {data.map((_, i) => (
            <Cell key={i} fill={colors[i % colors.length]} />
          ))}
        </Pie>
      </PieChart>
    );
  } else if (type === "radar") {
    inner = (
      <RadarChart data={data}>
        {showGrid ? <PolarGrid stroke="#2c2c2c" /> : null}
        <PolarAngleAxis dataKey={x} tick={{ fontSize: 11, fill: "#9a9a9a" }} />
        <PolarRadiusAxis tick={{ fontSize: 10, fill: "#6a6a6a" }} />
        <Tooltip {...tooltipProps} />
        {multiLegend}
        {ys.map((y, i) => (
          <Radar
            key={y}
            dataKey={y}
            stroke={colors[i]}
            fill={colors[i]}
            fillOpacity={0.45}
            isAnimationActive={false}
          />
        ))}
      </RadarChart>
    );
  } else if (type === "line") {
    inner = (
      <LineChart data={data} margin={chartMargin}>
        {grid}
        <XAxis
          dataKey={x}
          {...axisProps}
          axisLine={{ stroke: "#333" }}
          interval="preserveStartEnd"
          minTickGap={14}
        />
        <YAxis {...axisProps} axisLine={false} width={46} />
        <Tooltip {...tooltipProps} />
        {multiLegend}
        {ys.map((y, i) => (
          <Line
            key={y}
            type={curve}
            dataKey={y}
            stroke={colors[i]}
            strokeWidth={2}
            dot={!!opts.dots}
            isAnimationActive={false}
          >
            {opts.show_labels ? <LabelList dataKey={y} position="top" {...labelProps} /> : null}
          </Line>
        ))}
      </LineChart>
    );
  } else if (type === "area") {
    inner = (
      <AreaChart data={data} margin={chartMargin}>
        {grid}
        <XAxis
          dataKey={x}
          {...axisProps}
          axisLine={{ stroke: "#333" }}
          interval="preserveStartEnd"
          minTickGap={14}
        />
        <YAxis {...axisProps} axisLine={false} width={46} />
        <Tooltip {...tooltipProps} />
        {multiLegend}
        {ys.map((y, i) => (
          <Area
            key={y}
            type={curve}
            dataKey={y}
            stroke={colors[i]}
            fill={colors[i]}
            fillOpacity={0.22}
            strokeWidth={2}
            stackId={stackId}
            isAnimationActive={false}
          >
            {opts.show_labels ? <LabelList dataKey={y} position="top" {...labelProps} /> : null}
          </Area>
        ))}
      </AreaChart>
    );
  } else {
    // bar (default)
    const horiz = !!opts.horizontal;
    const r = opts.bar_radius || 0;
    const radius = horiz ? [0, r, r, 0] : [r, r, 0, 0];
    inner = (
      <BarChart data={data} margin={chartMargin} layout={horiz ? "vertical" : "horizontal"}>
        {showGrid ? (
          <CartesianGrid
            vertical={horiz}
            horizontal={!horiz}
            strokeDasharray="3 3"
            stroke="#2c2c2c"
          />
        ) : null}
        {horiz ? (
          <XAxis type="number" {...axisProps} axisLine={{ stroke: "#333" }} />
        ) : (
          <XAxis
            type="category"
            dataKey={x}
            {...axisProps}
            axisLine={{ stroke: "#333" }}
            interval="preserveStartEnd"
            minTickGap={10}
          />
        )}
        {horiz ? (
          <YAxis type="category" dataKey={x} {...axisProps} axisLine={false} width={104} />
        ) : (
          <YAxis type="number" {...axisProps} axisLine={false} width={46} />
        )}
        <Tooltip {...tooltipProps} />
        {multiLegend}
        {ys.map((y, i) => (
          <Bar
            key={y}
            dataKey={y}
            fill={colors[i]}
            radius={radius}
            stackId={stackId}
            isAnimationActive={false}
          >
            {opts.show_labels ? (
              <LabelList dataKey={y} position={horiz ? "right" : "top"} {...labelProps} />
            ) : null}
          </Bar>
        ))}
      </BarChart>
    );
  }

  return (
    <div style={{ width: "100%", height: height }}>
      <ResponsiveContainer width="100%" height="100%">
        {inner}
      </ResponsiveContainer>
    </div>
  );
}

// LiveChart fetches a saved chart's current data and renders it. Pass a
// changing refreshKey to force a re-fetch.
export function LiveChart(props) {
  const height = props.height || 260;
  const [st, setSt] = React.useState({ loading: true });

  React.useEffect(() => {
    let alive = true;
    setSt({ loading: true });
    api
      .chartData(props.chartId)
      .then((d) => {
        if (alive) setSt({ loading: false, data: d });
      })
      .catch((e) => {
        if (alive) setSt({ loading: false, error: String((e && e.message) || e) });
      });
    return () => {
      alive = false;
    };
  }, [props.chartId, props.refreshKey]);

  if (st.loading) return centered(height, "Loading chart…");
  if (st.error) return <ErrorText>{st.error}</ErrorText>;
  if (st.data && st.data.error) return <ErrorText>Query failed: {st.data.error}</ErrorText>;
  return <ChartView chart={st.data.chart} rows={st.data.rows} height={height} />;
}
