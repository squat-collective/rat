/*
 * rat-plugin-diff — portal UI bundle.
 *
 * Two views:
 *   /x/diff — activity feed (default). Filterable by kind, expandable
 *             before/after JSON per event.
 *   Drill-in (modal-ish overlay): for table-level events, pick two
 *             Iceberg snapshots → row-diff table.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[diff] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;
  var useState = React.useState;
  var useEffect = React.useEffect;
  var useCallback = React.useCallback;

  var C = {
    border: "hsl(var(--border, 0 0% 16%))",
    fg: "hsl(var(--foreground, 0 0% 90%))",
    muted: "hsl(var(--muted-foreground, 0 0% 50%))",
    card: "hsl(var(--card, 0 0% 7%))",
    bg: "hsl(var(--background, 0 0% 4%))",
    primary: "hsl(var(--primary, 142 72% 45%))",
    accent: "hsl(var(--accent, 280 50% 55%))",
    danger: "hsl(var(--destructive, 0 62% 35%))",
    warn: "hsl(var(--warning, 38 92% 50%))",
    add: "hsl(142 60% 45%)",
    rm: "hsl(0 62% 50%)",
  };

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/diff/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var API = apiBase() + "/api/v1/x/diff";

  function req(url, method, body) {
    var opts = { method: method || "GET", headers: {} };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(url, opts).then(function (res) {
      return res.text().then(function (t) {
        var d = null;
        try { d = t ? JSON.parse(t) : null; } catch (e) { d = { error: t }; }
        if (!res.ok) throw new Error((d && d.error) || ("HTTP " + res.status));
        return d;
      });
    });
  }

  function btn(bg, fg, extra) {
    return Object.assign({
      padding: "4px 10px", background: bg, color: fg,
      border: "1px solid " + C.border, fontSize: 11,
      cursor: "pointer", letterSpacing: 0.5, fontFamily: "inherit",
    }, extra || {});
  }
  function relativeTime(iso) {
    if (!iso) return "";
    var diff = (Date.now() - new Date(iso).getTime()) / 1000;
    if (diff < 60) return Math.round(diff) + "s ago";
    if (diff < 3600) return Math.round(diff / 60) + "m ago";
    if (diff < 86400) return Math.round(diff / 3600) + "h ago";
    return Math.round(diff / 86400) + "d ago";
  }

  // Kind → {icon, hue}. We don't ship icon fonts; tiny emoji-free
  // unicode glyphs are enough for at-a-glance scanning.
  var KIND_VISUAL = {
    "plugin.registered":      { mark: "+", color: C.add,    group: "plugin" },
    "plugin.unregistered":    { mark: "-", color: C.rm,     group: "plugin" },
    "plugin.config_changed":  { mark: "~", color: C.accent, group: "plugin" },
    "plugin.health_changed":  { mark: "*", color: C.warn,   group: "plugin" },
    "pipeline.created":       { mark: "+", color: C.add,    group: "pipeline" },
    "pipeline.deleted":       { mark: "-", color: C.rm,     group: "pipeline" },
    "schedule.created":       { mark: "+", color: C.add,    group: "schedule" },
    "schedule.deleted":       { mark: "-", color: C.rm,     group: "schedule" },
    "schedule.toggled":       { mark: "~", color: C.accent, group: "schedule" },
    "secret.created":         { mark: "+", color: C.add,    group: "secret" },
    "secret.deleted":         { mark: "-", color: C.rm,     group: "secret" },
    "secret.rotated":         { mark: "~", color: C.accent, group: "secret" },
    "run.completed":          { mark: ">", color: C.muted,  group: "run" },
    "namespace.created":      { mark: "+", color: C.add,    group: "namespace" },
    "namespace.deleted":      { mark: "-", color: C.rm,     group: "namespace" },
    "table.created":          { mark: "+", color: C.add,    group: "table" },
    "table.deleted":          { mark: "-", color: C.rm,     group: "table" },
    "table.rows_changed":     { mark: "~", color: C.accent, group: "table" },
  };

  function prettyJSON(v) {
    if (v === null || v === undefined) return "";
    try { return JSON.stringify(v, null, 2); }
    catch (e) { return String(v); }
  }

  // ── Unified line-diff (LCS) ──────────────────────────────────
  //
  // Standard O(N*M) DP, more than fast enough for the JSON blobs we
  // ever see in a plugin config or event payload. Returns a list of
  // hunks suitable for rendering as a unified diff: each item is
  // { type: ' '|'-'|'+', text, aLine?, bLine? }.

  function lcsDiff(aLines, bLines) {
    var n = aLines.length, m = bLines.length;
    if (n === 0 && m === 0) return [];
    // Allocate dp table. For very small diffs this is fine. We cap at
    // 2000 lines/side as a safety valve — anything bigger we fall back
    // to a non-aligned "all removed / all added" render.
    if (n > 2000 || m > 2000) {
      var fallback = [];
      for (var i = 0; i < n; i++) fallback.push({ type: "-", text: aLines[i], aLine: i });
      for (var j = 0; j < m; j++) fallback.push({ type: "+", text: bLines[j], bLine: j });
      return fallback;
    }
    var dp = new Array(n + 1);
    for (var i = 0; i <= n; i++) dp[i] = new Int32Array(m + 1);
    for (var i = 1; i <= n; i++) {
      for (var j = 1; j <= m; j++) {
        if (aLines[i - 1] === bLines[j - 1]) dp[i][j] = dp[i - 1][j - 1] + 1;
        else dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
      }
    }
    var out = [];
    var ii = n, jj = m;
    while (ii > 0 && jj > 0) {
      if (aLines[ii - 1] === bLines[jj - 1]) {
        out.unshift({ type: " ", text: aLines[ii - 1], aLine: ii - 1, bLine: jj - 1 });
        ii--; jj--;
      } else if (dp[ii - 1][jj] >= dp[ii][jj - 1]) {
        out.unshift({ type: "-", text: aLines[ii - 1], aLine: ii - 1 });
        ii--;
      } else {
        out.unshift({ type: "+", text: bLines[jj - 1], bLine: jj - 1 });
        jj--;
      }
    }
    while (ii > 0) { out.unshift({ type: "-", text: aLines[ii - 1], aLine: ii - 1 }); ii--; }
    while (jj > 0) { out.unshift({ type: "+", text: bLines[jj - 1], bLine: jj - 1 }); jj--; }
    return out;
  }

  function UnifiedDiff(props) {
    var beforeStr = prettyJSON(props.before);
    var afterStr  = prettyJSON(props.after);
    var aLines = beforeStr ? beforeStr.split("\n") : [];
    var bLines = afterStr ? afterStr.split("\n") : [];
    var lines = lcsDiff(aLines, bLines);

    // Tally for the header.
    var added = 0, removed = 0;
    for (var i = 0; i < lines.length; i++) {
      if (lines[i].type === "+") added++;
      else if (lines[i].type === "-") removed++;
    }
    if (lines.length === 0) {
      return h("div", { style: { color: C.muted, fontSize: 11, padding: 6 } }, "no payload");
    }
    if (added === 0 && removed === 0) {
      return h("div", { style: { color: C.muted, fontSize: 11, padding: 6 } }, "no differences");
    }

    // Render. Two narrow line-number gutters (A, B) then the line itself.
    return h("div", {
      style: {
        background: C.card, border: "1px solid " + C.border,
        marginTop: 4, maxHeight: 360, overflow: "auto",
        fontFamily: "monospace", fontSize: 11,
      },
    },
      h("div", {
        style: {
          padding: "4px 8px", borderBottom: "1px solid " + C.border,
          color: C.muted, fontSize: 10, display: "flex", gap: 12,
        },
      },
        h("span", { style: { color: C.add } }, "+" + added + " added"),
        h("span", { style: { color: C.rm } }, "-" + removed + " removed"),
        h("span", null, lines.length + " lines total"),
      ),
      h("div", null, lines.map(function (ln, idx) {
        var bg = "transparent", color = C.fg, lnA = "", lnB = "";
        if (ln.type === "+") { bg = "rgba(34,197,94,0.10)"; color = C.add; lnB = String(ln.bLine + 1); }
        else if (ln.type === "-") { bg = "rgba(239,68,68,0.10)"; color = C.rm; lnA = String(ln.aLine + 1); }
        else { color = C.muted; lnA = String(ln.aLine + 1); lnB = String(ln.bLine + 1); }
        return h("div", {
          key: idx,
          style: { display: "flex", background: bg, lineHeight: "16px" },
        },
          h("span", {
            style: {
              width: 32, padding: "0 4px", textAlign: "right",
              color: C.muted, opacity: 0.6, userSelect: "none",
              borderRight: "1px solid " + C.border,
            },
          }, lnA),
          h("span", {
            style: {
              width: 32, padding: "0 4px", textAlign: "right",
              color: C.muted, opacity: 0.6, userSelect: "none",
              borderRight: "1px solid " + C.border,
            },
          }, lnB),
          h("span", {
            style: {
              width: 18, padding: "0 4px", textAlign: "center",
              color: color, fontWeight: 700, userSelect: "none",
            },
          }, ln.type === " " ? "" : ln.type),
          h("span", {
            style: { flex: 1, padding: "0 8px", color: color, whiteSpace: "pre-wrap", wordBreak: "break-all" },
          }, ln.text || " "),
        );
      })),
    );
  }

  // ── Drill-in: table snapshots + row diff ──────────────────────

  function TableDiffer(props) {
    var s1 = useState([]), snaps = s1[0], setSnaps = s1[1];
    var s2 = useState(null), err = s2[0], setErr = s2[1];
    // Index into the snapshots list (newest = 0). Easier than passing
    // the full {metadata_url, snapshot_id} pair around.
    var s3 = useState(1), aIdx = s3[0], setA = s3[1];
    var s4 = useState(0), bIdx = s4[0], setB = s4[1];
    var s5 = useState(false), busy = s5[0], setBusy = s5[1];
    var s6 = useState(null), diff = s6[0], setDiff = s6[1];
    var s7 = useState(""), pkOverride = s7[0], setPK = s7[1];

    useEffect(function () {
      setErr(null); setSnaps([]);
      req(API + "/tables/" + props.ns + "/" + props.layer + "/" + props.name + "/snapshots")
        .then(function (r) {
          var list = (r && r.snapshots) || [];
          setSnaps(list);
          setB(0);
          setA(list.length >= 2 ? 1 : 0);
        })
        .catch(function (e) { setErr(e.message); });
    }, [props.ns, props.layer, props.name]);

    function doDiff() {
      var a = snaps[Number(aIdx)], b = snaps[Number(bIdx)];
      if (!a || !b) return;
      setBusy(true); setErr(null); setDiff(null);
      req(API + "/tables/" + props.ns + "/" + props.layer + "/" + props.name + "/diff", "POST", {
        a: { metadata_url: a.metadata_location, snapshot_id: a.snapshot_id },
        b: { metadata_url: b.metadata_location, snapshot_id: b.snapshot_id },
        limit: 200,
      })
        .then(function (r) { setDiff(r); setBusy(false); })
        .catch(function (e) { setErr(e.message); setBusy(false); });
    }

    return h("div", {
      style: {
        position: "fixed", inset: 0, background: "rgba(0,0,0,0.7)", zIndex: 50,
        display: "flex", alignItems: "stretch", justifyContent: "center",
      },
      onClick: function (e) { if (e.target === e.currentTarget) props.onClose(); },
    },
      h("div", {
        style: {
          background: C.bg, border: "1px solid " + C.border,
          width: "min(1100px, 95vw)", margin: "30px auto",
          display: "flex", flexDirection: "column", maxHeight: "calc(100vh - 60px)",
        },
      },
        h("div", { style: { padding: 12, borderBottom: "1px solid " + C.border, display: "flex", alignItems: "center", gap: 12 } },
          h("div", { style: { fontWeight: 700, fontSize: 13 } }, "Row diff —"),
          h("code", { style: { color: C.accent } }, props.ns + "." + props.layer + "." + props.name),
          h("div", { style: { marginLeft: "auto" } },
            h("button", { onClick: props.onClose, style: btn("transparent", C.fg) }, "close")),
        ),

        h("div", { style: { padding: 12, borderBottom: "1px solid " + C.border, display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap" } },
          err && h("div", { style: { color: C.danger, fontSize: 12 } }, err),
          snaps.length === 0 && !err && h("div", { style: { color: C.muted, fontSize: 12 } }, "loading snapshots…"),
          snaps.length === 1 && h("div", { style: { color: C.warn, fontSize: 11 } },
            "only 1 commit recorded so far — can't diff against itself; run a pipeline to create another version"),
          snaps.length > 1 && [
            h("label", { key: "la", style: { fontSize: 11, color: C.muted } }, "A (older):"),
            h("select", {
              key: "a", value: String(aIdx),
              onChange: function (e) { setA(e.target.value); },
              style: { fontFamily: "monospace", background: C.bg, color: C.fg, border: "1px solid " + C.border, padding: 4, fontSize: 12, maxWidth: 320 },
            }, snaps.map(function (s, i) {
              return h("option", { key: i, value: String(i) },
                "#" + (snaps.length - i) + "  " + new Date(s.committed_at).toLocaleString());
            })),
            h("label", { key: "lb", style: { fontSize: 11, color: C.muted } }, "B (newer):"),
            h("select", {
              key: "b", value: String(bIdx),
              onChange: function (e) { setB(e.target.value); },
              style: { fontFamily: "monospace", background: C.bg, color: C.fg, border: "1px solid " + C.border, padding: 4, fontSize: 12, maxWidth: 320 },
            }, snaps.map(function (s, i) {
              return h("option", { key: i, value: String(i) },
                "#" + (snaps.length - i) + "  " + new Date(s.committed_at).toLocaleString());
            })),
            h("button", {
              key: "go", onClick: doDiff, disabled: busy || aIdx === bIdx,
              style: btn(busy || aIdx === bIdx ? C.muted : C.primary, C.bg),
            }, busy ? "computing…" : "compute diff"),
          ],
        ),

        h("div", { style: { flex: 1, overflow: "auto", padding: 12 } },
          diff && h(DiffResult, {
            diff: diff,
            pkOverride: pkOverride,
            onChangePK: setPK,
          }),
        ),
      ),
    );

    function DiffResult(props) {
      var d = props.diff;
      var cols = d.columns || [];
      var defaultPK = guessPK(cols);
      var pk = props.pkOverride || defaultPK;
      var reconciled = reconcileByPK(d.added || [], d.removed || [], cols, pk);

      var totalSignal = reconciled.added.length + reconciled.removed.length + reconciled.modified.length;

      return h("div", null,
        // Header with stats + PK picker.
        h("div", { style: { display: "flex", alignItems: "center", gap: 12, marginBottom: 10, fontSize: 12, flexWrap: "wrap" } },
          reconciled.modified.length > 0 && h("span", { style: { color: C.accent } }, "~ " + reconciled.modified.length + " modified"),
          reconciled.added.length > 0 && h("span", { style: { color: C.add } }, "+ " + reconciled.added.length + " added"),
          reconciled.removed.length > 0 && h("span", { style: { color: C.rm } }, "- " + reconciled.removed.length + " removed"),
          d.stats && d.stats.truncated && h("span", { style: { color: C.warn } }, "(truncated at 200/side)"),
          h("div", { style: { marginLeft: "auto", display: "flex", alignItems: "center", gap: 6 } },
            h("span", { style: { color: C.muted, fontSize: 11 } }, "match rows by:"),
            h("select", {
              value: pk || "", onChange: function (e) { props.onChangePK(e.target.value); },
              style: { fontFamily: "monospace", background: C.bg, color: C.fg, border: "1px solid " + C.border, padding: 3, fontSize: 11 },
            },
              cols.map(function (c) {
                return h("option", { key: c, value: c }, c + (c === defaultPK ? "  (guessed)" : ""));
              }),
            ),
          ),
        ),

        totalSignal === 0 && h("div", { style: { color: C.muted, fontSize: 12, padding: 12 } },
          "no row-level differences between these snapshots"),

        // Modified rows (the most interesting bit).
        reconciled.modified.length > 0 && h("div", { style: { marginBottom: 16 } },
          h("div", { style: { color: C.accent, fontSize: 11, fontWeight: 700, marginBottom: 4 } },
            "~ modified (" + reconciled.modified.length + ")"),
          reconciled.modified.map(function (m, i) {
            return h("div", { key: i, style: { border: "1px solid " + C.border, marginBottom: 6 } },
              h("div", { style: { padding: "4px 8px", background: C.card, fontSize: 11, fontFamily: "monospace", color: C.muted, borderBottom: "1px solid " + C.border } },
                pk + " = ", h("strong", { style: { color: C.fg } }, String(m.key))),
              h(ModifiedRowDiff, { fields: m.fields }),
            );
          }),
        ),

        renderDiffTable({ columns: cols, added: reconciled.added }, "added", C.add, "+"),
        renderDiffTable({ columns: cols, removed: reconciled.removed }, "removed", C.rm, "-"),
      );
    }

    function renderDiffTable(d, key, color, mark) {
      var rows = d[key];
      if (!rows || rows.length === 0) return null;
      return h("div", { style: { marginBottom: 16 } },
        h("div", { style: { color: color, fontSize: 11, fontWeight: 700, marginBottom: 4 } },
          mark + " " + key + " (" + rows.length + ")"),
        h("div", { style: { overflowX: "auto", border: "1px solid " + C.border } },
          h("table", { style: { borderCollapse: "collapse", fontSize: 11, fontFamily: "monospace", width: "100%" } },
            h("thead", null, h("tr", { style: { background: C.card } },
              h("th", { style: { padding: 4, textAlign: "left", color: C.muted, borderBottom: "1px solid " + C.border, width: 24 } }, ""),
              d.columns.map(function (c) {
                return h("th", { key: c, style: { padding: "4px 8px", textAlign: "left", color: C.muted, borderBottom: "1px solid " + C.border } }, c);
              }),
            )),
            h("tbody", null, rows.map(function (r, i) {
              return h("tr", { key: i, style: { borderBottom: "1px solid " + C.border } },
                h("td", { style: { padding: 4, color: color, fontWeight: 700, textAlign: "center" } }, mark),
                d.columns.map(function (c) {
                  return h("td", { key: c, style: { padding: "4px 8px", color: C.fg } }, formatCell(r[c]));
                }),
              );
            })),
          )),
      );
    }
    function formatCell(v) {
      if (v === null || v === undefined) return h("span", { style: { color: C.muted } }, "null");
      if (typeof v === "object") return JSON.stringify(v);
      return String(v);
    }
  }

  // ── PK-aware row reconciliation ────────────────────────────────
  //
  // EXCEPT gives us set-difference: a row whose `name` field was edited
  // shows up as 1 removed + 1 added with no signal they're the same
  // entity. Joining the two sides by a primary key lets us classify
  // each row as truly added / truly removed / modified — and for
  // modified rows, show which columns changed.

  function guessPK(columns) {
    if (!columns || columns.length === 0) return null;
    // Common identifier names first.
    var preferred = ["id", "uuid", "pk", "primary_key", "key"];
    for (var i = 0; i < preferred.length; i++) {
      if (columns.indexOf(preferred[i]) !== -1) return preferred[i];
    }
    for (var i = 0; i < columns.length; i++) {
      if (/_id$|^id_/i.test(columns[i])) return columns[i];
    }
    return columns[0]; // fallback: first column
  }

  function reconcileByPK(added, removed, columns, pk) {
    if (!pk) return { added: added, removed: removed, modified: [] };
    var prevByKey = {};
    removed.forEach(function (r) {
      var k = r[pk];
      if (k === undefined || k === null) return;
      prevByKey[String(k)] = r;
    });
    var truelyAdded = [], modified = [];
    var matched = {};
    added.forEach(function (r) {
      var k = r[pk];
      if (k !== undefined && k !== null && prevByKey[String(k)]) {
        matched[String(k)] = true;
        var before = prevByKey[String(k)];
        var fieldDiffs = [];
        columns.forEach(function (c) {
          if (!sameValue(before[c], r[c])) {
            fieldDiffs.push({ col: c, before: before[c], after: r[c] });
          }
        });
        if (fieldDiffs.length > 0) {
          modified.push({ key: k, before: before, after: r, fields: fieldDiffs });
        }
        // If fields is empty the row matched bit-for-bit on the joined
        // columns — shouldn't happen via EXCEPT but harmless to skip.
      } else {
        truelyAdded.push(r);
      }
    });
    var truelyRemoved = removed.filter(function (r) {
      var k = r[pk];
      return !(k !== undefined && k !== null && matched[String(k)]);
    });
    return { added: truelyAdded, removed: truelyRemoved, modified: modified };
  }

  function sameValue(a, b) {
    if (a === b) return true;
    if (a === null || b === null || a === undefined || b === undefined) return false;
    if (typeof a === "object" && typeof b === "object") {
      try { return JSON.stringify(a) === JSON.stringify(b); }
      catch (e) { return false; }
    }
    return false;
  }

  function ModifiedRowDiff(props) {
    return h("div", { style: { display: "grid", gridTemplateColumns: "auto 1fr 1fr", gap: 0, fontFamily: "monospace", fontSize: 11 } },
      h("div", { style: { padding: "4px 8px", color: C.muted, borderBottom: "1px solid " + C.border } }, "column"),
      h("div", { style: { padding: "4px 8px", color: C.rm, borderBottom: "1px solid " + C.border } }, "before"),
      h("div", { style: { padding: "4px 8px", color: C.add, borderBottom: "1px solid " + C.border } }, "after"),
      props.fields.map(function (f, i) {
        return [
          h("div", { key: "c" + i, style: { padding: "4px 8px", color: C.muted, borderBottom: "1px solid " + C.border } }, f.col),
          h("div", { key: "b" + i, style: { padding: "4px 8px", color: C.rm, background: "rgba(239,68,68,0.06)", borderBottom: "1px solid " + C.border, wordBreak: "break-all" } }, formatVal(f.before)),
          h("div", { key: "a" + i, style: { padding: "4px 8px", color: C.add, background: "rgba(34,197,94,0.06)", borderBottom: "1px solid " + C.border, wordBreak: "break-all" } }, formatVal(f.after)),
        ];
      }).flat(),
    );
  }
  function formatVal(v) {
    if (v === null || v === undefined) return "null";
    if (typeof v === "object") return JSON.stringify(v);
    return String(v);
  }

  // ── Feed item ─────────────────────────────────────────────────

  function EventRow(props) {
    var s = useState(false), open = s[0], setOpen = s[1];
    var e = props.event;
    var visual = KIND_VISUAL[e.kind] || { mark: "?", color: C.muted, group: "?" };

    // For table-level events we offer a "diff rows" deep link.
    var tableDeepLink = null;
    if (e.metadata && e.metadata.table) {
      var parts = e.metadata.table.split(".");
      if (parts.length === 3) {
        tableDeepLink = {
          ns: parts[0], layer: parts[1], name: parts[2],
        };
      }
    }

    return h("div", { style: { borderBottom: "1px solid " + C.border, padding: "8px 10px" } },
      h("div", { style: { display: "flex", alignItems: "center", gap: 10 } },
        h("span", {
          style: {
            display: "inline-block", width: 18, textAlign: "center",
            color: visual.color, fontWeight: 700, fontSize: 16, fontFamily: "monospace",
          },
        }, visual.mark),
        h("div", { style: { flex: 1, minWidth: 0 } },
          h("div", { style: { fontSize: 12 } }, e.summary),
          h("div", { style: { fontSize: 10, color: C.muted, marginTop: 2 } },
            relativeTime(e.time), " · ",
            h("code", { style: { color: C.muted } }, e.kind),
          ),
        ),
        tableDeepLink && h("button", {
          onClick: function () { props.onOpenDiff(tableDeepLink); },
          style: btn("transparent", C.accent),
        }, "diff rows"),
        (e.before || e.after) && h("button", {
          onClick: function () { setOpen(!open); },
          style: btn("transparent", C.muted),
        }, open ? "hide" : "details"),
      ),
      open && (e.before !== undefined || e.after !== undefined) &&
        h(UnifiedDiff, { before: e.before, after: e.after }),
    );
  }

  // ── App ────────────────────────────────────────────────────────

  function DiffApp() {
    var s1 = useState([]), events = s1[0], setEvents = s1[1];
    var s2 = useState(""), filter = s2[0], setFilter = s2[1];
    var s3 = useState(null), drill = s3[0], setDrill = s3[1];
    var s4 = useState(null), err = s4[0], setErr = s4[1];

    var refresh = useCallback(function () {
      var url = API + "/events?limit=200";
      if (filter) url += "&kind=" + encodeURIComponent(filter);
      req(url).then(function (r) {
        setEvents((r && r.events) || []);
        setErr(null);
      }).catch(function (e) { setErr(e.message); });
    }, [filter]);

    useEffect(function () {
      refresh();
      var t = setInterval(refresh, 5000);
      return function () { clearInterval(t); };
    }, [refresh]);

    var KINDS = [
      { v: "",          label: "all" },
      { v: "plugin.",   label: "plugins" },
      { v: "pipeline.", label: "pipelines" },
      { v: "schedule.", label: "schedules" },
      { v: "secret.",   label: "secrets" },
      { v: "namespace.",label: "namespaces" },
      { v: "table.",    label: "tables" },
      { v: "run.",      label: "runs" },
    ];

    return h("div", { style: { padding: 20, color: C.fg, background: C.bg, minHeight: "calc(100vh - 60px)" } },
      h("div", { style: { display: "flex", alignItems: "center", gap: 12, marginBottom: 12 } },
        h("h1", { style: { margin: 0, fontSize: 16, letterSpacing: 1, fontWeight: 700 } }, "DIFF"),
        h("span", { style: { color: C.muted, fontSize: 12 } }, "live activity feed · polled every 15s"),
        h("div", { style: { marginLeft: "auto", fontSize: 11, color: C.muted } },
          events.length + " events"),
      ),

      h("div", { style: { display: "flex", gap: 4, marginBottom: 12, borderBottom: "1px solid " + C.border, paddingBottom: 8, flexWrap: "wrap" } },
        KINDS.map(function (k) {
          var active = filter === k.v;
          return h("button", {
            key: k.v,
            onClick: function () { setFilter(k.v); },
            style: btn(active ? C.primary : "transparent", active ? C.bg : C.fg, { fontWeight: active ? 700 : 400 }),
          }, k.label);
        }),
      ),

      err && h("div", { style: { padding: 8, marginBottom: 8, color: C.danger, fontSize: 12, border: "1px solid " + C.danger } }, err),

      events.length === 0
        ? h("div", { style: { padding: 24, color: C.muted, fontSize: 13, textAlign: "center", border: "1px dashed " + C.border } },
            "no events yet — interact with the system and they'll start streaming in")
        : h("div", { style: { border: "1px solid " + C.border, background: C.card } },
            events.map(function (e) {
              return h(EventRow, { key: e.id, event: e, onOpenDiff: setDrill });
            })),

      drill && h(TableDiffer, {
        ns: drill.ns, layer: drill.layer, name: drill.name,
        onClose: function () { setDrill(null); },
      }),
    );
  }

  window.__RAT_REGISTER_PLUGIN("diff", {
    navItems: [{ label: "Diff", icon: "git-compare", href: "/x/diff", priority: 14 }],
    routes: [{ path: "/x/diff", component: DiffApp }],
  });
  console.info("[diff] registered with the portal");
})();
