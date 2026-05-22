// Automatic-JSX-runtime shim, implemented on top of the portal's React. Used
// by any dependency (e.g. Recharts) compiled with the automatic JSX runtime.
var React = window.React;

function jsx(type, props, key) {
  props = props || {};
  var rest = {};
  for (var k in props) {
    if (k !== "children") rest[k] = props[k];
  }
  if (key !== undefined) rest.key = key;

  var children = props.children;
  if (children === undefined) return React.createElement(type, rest);
  if (Array.isArray(children)) {
    return React.createElement.apply(null, [type, rest].concat(children));
  }
  return React.createElement(type, rest, children);
}

exports.Fragment = React.Fragment;
exports.jsx = jsx;
exports.jsxs = jsx;
exports.jsxDEV = jsx;
