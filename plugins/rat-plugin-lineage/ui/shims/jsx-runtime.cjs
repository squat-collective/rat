// React's automatic JSX runtime maps `<Foo />` to `jsx(Foo, …)`.
// Re-implement using createElement from the host's React.
"use strict";
const R = (typeof window !== "undefined" && window.React) || {};
const createElement = R.createElement;
const Fragment = R.Fragment;

function jsx(type, props, key) {
  const { children, ...rest } = props || {};
  if (key !== undefined) rest.key = key;
  if (children === undefined) return createElement(type, rest);
  return createElement(type, rest, children);
}

// jsxs is the variant for static children arrays — same semantics for
// our purposes (createElement accepts arrays just fine).
module.exports = { jsx, jsxs: jsx, jsxDEV: jsx, Fragment };
