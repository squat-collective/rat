// The chart renderer — real Recharts, themed to the RAT portal. ChartView
// draws a chart spec from a set of rows; LiveChart additionally fetches the
// rows by re-running the chart's saved query (the "live" behaviour).

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
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from "recharts";
import { api } from "./api.js";
import { C, ErrorText } from "./components.jsx";

// Series palette — plain hex (SVG fill attributes don't resolve CSS vars).
export const PALETTE = [
  "#4ade80",
  "#60a5fa",
  "#f472b6",
  "#fbbf24",
  "#a78bfa",
  "#22d3ee",
  "#fb923c",
  "#34d399",
];

const chartMargin = { top: 8, right: 14, bottom: 4, left: 0 };
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

// ChartView draws one chart from an explicit { chart, rows } pair.
export function ChartView(props) {
  const chart = props.chart || {};
  const height = props.height || 260;
  const rows = props.rows || [];
  const x = chart.x_column;
  const ys = chart.y_columns && chart.y_columns.length ? chart.y_columns : [];

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

  const type = chart.type || "bar";
  const grid = <CartesianGrid vertical={false} strokeDasharray="3 3" stroke="#2c2c2c" />;
  const xAxis = (
    <XAxis
      dataKey={x}
      {...axisProps}
      axisLine={{ stroke: "#333" }}
      interval="preserveStartEnd"
      minTickGap={14}
    />
  );
  const yAxis = <YAxis {...axisProps} axisLine={false} width={46} />;
  const legend = ys.length > 1 ? <Legend wrapperStyle={{ fontSize: "0.72rem" }} /> : null;

  let inner;
  if (type === "pie") {
    inner = (
      <PieChart>
        <Tooltip {...tooltipProps} />
        <Legend wrapperStyle={{ fontSize: "0.72rem" }} />
        <Pie
          data={data}
          dataKey={ys[0]}
          nameKey={x}
          cx="50%"
          cy="50%"
          outerRadius={Math.max(54, height * 0.34)}
          isAnimationActive={false}
          label={(e) => e.name}
        >
          {data.map((_, i) => (
            <Cell key={i} fill={PALETTE[i % PALETTE.length]} />
          ))}
        </Pie>
      </PieChart>
    );
  } else if (type === "line") {
    inner = (
      <LineChart data={data} margin={chartMargin}>
        {grid}
        {xAxis}
        {yAxis}
        <Tooltip {...tooltipProps} />
        {legend}
        {ys.map((y, i) => (
          <Line
            key={y}
            type="monotone"
            dataKey={y}
            stroke={PALETTE[i % PALETTE.length]}
            strokeWidth={2}
            dot={false}
            isAnimationActive={false}
          />
        ))}
      </LineChart>
    );
  } else if (type === "area") {
    inner = (
      <AreaChart data={data} margin={chartMargin}>
        {grid}
        {xAxis}
        {yAxis}
        <Tooltip {...tooltipProps} />
        {legend}
        {ys.map((y, i) => (
          <Area
            key={y}
            type="monotone"
            dataKey={y}
            stroke={PALETTE[i % PALETTE.length]}
            fill={PALETTE[i % PALETTE.length]}
            fillOpacity={0.18}
            strokeWidth={2}
            isAnimationActive={false}
          />
        ))}
      </AreaChart>
    );
  } else {
    inner = (
      <BarChart data={data} margin={chartMargin}>
        {grid}
        {xAxis}
        {yAxis}
        <Tooltip {...tooltipProps} />
        {legend}
        {ys.map((y, i) => (
          <Bar
            key={y}
            dataKey={y}
            fill={PALETTE[i % PALETTE.length]}
            radius={[2, 2, 0, 0]}
            isAnimationActive={false}
          />
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
