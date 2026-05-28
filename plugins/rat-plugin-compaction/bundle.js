/*
 * rat-plugin-compaction — portal UI bundle.
 *
 * One screen: table grid showing each Iceberg table + file health stats.
 * Rows are sortable by file count, ratio, status. Manual "Compact" button
 * per row; a global "Scan now" button forces an immediate detection sweep.
 *
 * Build-free (no JSX, no bundler) — matches the rat-plugin-pg-sync shape.
 */
(function () {
  "use strict";

  var React = window.React;
  if (!React || typeof window.__RAT_REGISTER_PLUGIN !== "function") {
    console.warn("[compaction] RAT plugin host not available — skipping");
    return;
  }
  var h = React.createElement;
  var useState = React.useState;
  var useEffect = React.useEffect;

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
    ok: "hsl(142 60% 45%)",
  };

  function apiBase() {
    var s = document.querySelector('script[src*="/plugins/compaction/ui/bundle.js"]');
    if (s && s.src && s.src.indexOf("/api/v1/") !== -1) {
      return s.src.slice(0, s.src.indexOf("/api/v1/"));
    }
    return window.location.origin;
  }
  var API = apiBase() + "/api/v1/x/compaction";

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
    if (!iso || iso.startsWith("0001-")) return "never";
    var diff = (Date.now() - new Date(iso).getTime()) / 1000;
    if (diff < 60) return Math.round(diff) + "s ago";
    if (diff < 3600) return Math.round(diff / 60) + "m ago";
    if (diff < 86400) return Math.round(diff / 3600) + "h ago";
    return Math.round(diff / 86400) + "d ago";
  }
  function fmtBytes(n) {
    if (!n) return "0";
    if (n < 1024) return n + " B";
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KiB";
    if (n < 1024 * 1024 * 1024) return (n / (1024 * 1024)).toFixed(1) + " MiB";
    return (n / (1024 * 1024 * 1024)).toFixed(2) + " GiB";
  }
  function statusBadge(status) {
    var bg, fg;
    switch (status) {
      case "ok":          bg = C.ok; fg = "#000"; break;
      case "candidate":   bg = C.warn; fg = "#000"; break;
      case "compacting":  bg = C.accent; fg = "#fff"; break;
      case "error":       bg = C.danger; fg = "#fff"; break;
      default:            bg = C.muted; fg = "#000";
    }
    return h("span", {
      style: {
        padding: "2px 8px", background: bg, color: fg,
        fontSize: 10, letterSpacing: 1, textTransform: "uppercase",
      },
    }, status || "?");
  }

  function CompactionApp() {
    var s = useState([]); var tables = s[0]; var setTables = s[1];
    var l = useState(true); var loading = l[0]; var setLoading = l[1];
    var e = useState(null); var err = e[0]; var setErr = e[1];
    var b = useState(false); var busy = b[0]; var setBusy = b[1];
    var sortBy = useState({ col: "small_file_ratio", dir: "desc" });
    var sort = sortBy[0]; var setSort = sortBy[1];

    function refresh() {
      req(API + "/tables")
        .then(function (d) {
          setTables(d.tables || []);
          setLoading(false);
          setErr(null);
        })
        .catch(function (er) { setErr(er.message); setLoading(false); });
    }

    function scanNow() {
      setBusy(true);
      req(API + "/scan", "POST")
        .then(function () { setBusy(false); refresh(); })
        .catch(function (er) { setErr(er.message); setBusy(false); });
    }

    function compact(t) {
      // Optimistic UI: flip status locally so the "Compact" button reflects
      // the queued state immediately. The next poll catches up with truth.
      setTables(tables.map(function (r) {
        if (r.namespace === t.namespace && r.layer === t.layer && r.name === t.name) {
          return Object.assign({}, r, { status: "compacting" });
        }
        return r;
      }));
      req(API + "/tables/" + t.namespace + "/" + t.layer + "/" + t.name + "/compact", "POST")
        .catch(function (er) { setErr(er.message); refresh(); });
    }

    useEffect(function () {
      refresh();
      var id = setInterval(refresh, 5000);
      return function () { clearInterval(id); };
    }, []);

    if (loading) {
      return h("div", { style: { padding: 32, color: C.muted } }, "Loading…");
    }

    // Sorting
    var sorted = tables.slice().sort(function (a, b) {
      var av = a[sort.col]; var bv = b[sort.col];
      if (typeof av === "string") { av = av.toLowerCase(); bv = (bv || "").toLowerCase(); }
      if (av === bv) return 0;
      var n = av > bv ? 1 : -1;
      return sort.dir === "desc" ? -n : n;
    });

    function header(label, col) {
      var active = sort.col === col;
      var arrow = active ? (sort.dir === "desc" ? " ↓" : " ↑") : "";
      return h("th", {
        style: {
          textAlign: "left", padding: "8px 10px", borderBottom: "1px solid " + C.border,
          color: active ? C.fg : C.muted, fontSize: 10, fontWeight: 600,
          textTransform: "uppercase", letterSpacing: 1, cursor: "pointer", userSelect: "none",
        },
        onClick: function () {
          if (active) setSort({ col: col, dir: sort.dir === "desc" ? "asc" : "desc" });
          else setSort({ col: col, dir: "desc" });
        },
      }, label + arrow);
    }

    var candidateCount = tables.filter(function (t) { return t.status === "candidate"; }).length;
    var totalFiles = tables.reduce(function (s, t) { return s + (t.file_count || 0); }, 0);

    return h("div", { style: { padding: 24, color: C.fg, fontFamily: "system-ui, sans-serif" } },
      // Header
      h("div", { style: { display: "flex", justifyContent: "space-between", alignItems: "flex-start", marginBottom: 16 } },
        h("div", null,
          h("h2", { style: { margin: 0, fontSize: 18, letterSpacing: 1 } }, "ICEBERG COMPACTION"),
          h("div", { style: { color: C.muted, fontSize: 12, marginTop: 4 } },
            tables.length + " tables monitored · " + totalFiles.toLocaleString() + " total parquet files · " +
            candidateCount + " compaction candidate" + (candidateCount === 1 ? "" : "s")),
        ),
        h("button", {
          style: btn(C.primary, "#000"),
          onClick: scanNow, disabled: busy,
        }, busy ? "Scanning…" : "Scan now"),
      ),

      err && h("div", {
        style: {
          padding: 10, background: C.danger, color: "#fff",
          marginBottom: 12, fontSize: 12,
        },
      }, err),

      // Table grid
      h("table", { style: { width: "100%", borderCollapse: "collapse", background: C.card } },
        h("thead", null, h("tr", null,
          header("Table", "name"),
          header("Files", "file_count"),
          header("Total size", "total_bytes"),
          header("Small-file ratio", "small_file_ratio"),
          header("Status", "status"),
          header("Last compacted", "last_compacted_at"),
          h("th", { style: { borderBottom: "1px solid " + C.border, padding: "8px 10px" } }, ""),
        )),
        h("tbody", null, sorted.length === 0
          ? h("tr", null, h("td", {
              colSpan: 7,
              style: { padding: 40, textAlign: "center", color: C.muted },
            }, "No tables discovered yet — try \"Scan now\"."))
          : sorted.map(function (t) {
            var key = t.namespace + "/" + t.layer + "/" + t.name;
            var compactable = t.status === "candidate" || (t.status === "ok" && t.file_count > 1);
            return h("tr", { key: key, style: { borderBottom: "1px solid " + C.border } },
              h("td", { style: { padding: "10px", fontFamily: "monospace", fontSize: 12 } },
                t.namespace + "." + t.layer + "." + t.name),
              h("td", { style: { padding: "10px", fontSize: 12, fontVariantNumeric: "tabular-nums" } },
                (t.file_count || 0).toLocaleString()),
              h("td", { style: { padding: "10px", fontSize: 12, fontVariantNumeric: "tabular-nums" } },
                fmtBytes(t.total_bytes)),
              h("td", { style: { padding: "10px", fontSize: 12 } },
                ((t.small_file_ratio || 0) * 100).toFixed(0) + "%"),
              h("td", { style: { padding: "10px" } }, statusBadge(t.status)),
              h("td", { style: { padding: "10px", fontSize: 12, color: C.muted } },
                relativeTime(t.last_compacted_at)),
              h("td", { style: { padding: "10px", textAlign: "right" } },
                h("button", {
                  style: btn(compactable ? C.primary : C.bg, compactable ? "#000" : C.muted,
                    { opacity: compactable && t.status !== "compacting" ? 1 : 0.5 }),
                  disabled: !compactable || t.status === "compacting",
                  onClick: function () { compact(t); },
                  title: t.last_compact_stats || "",
                }, t.status === "compacting" ? "Compacting…" : "Compact"),
              ),
            );
          })),
      ),

      // Footer note
      h("div", { style: { marginTop: 12, fontSize: 11, color: C.muted, fontStyle: "italic" } },
        "Tables flagged as ", h("strong", null, "candidate"),
        " exceed the small-file-ratio threshold (default 30%) and ",
        "auto-compact on the configured interval. Old data files remain in S3 ",
        "as unreferenced snapshots until snapshot-expiry runs."),
    );
  }

  window.__RAT_REGISTER_PLUGIN("compaction", {
    navItems: [{ label: "Compaction", icon: "boxes", href: "/x/compaction", priority: 14 }],
    routes: [{ path: "/x/compaction", component: CompactionApp }],
  });
  console.info("[compaction] registered with the portal");
})();
