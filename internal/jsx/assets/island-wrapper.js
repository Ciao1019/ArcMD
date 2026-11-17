
const {React,createRoot} = ArcMDReact;
const Content = (function(){__COMPILED_BODY__})(ArcMDReact).default;
const source = __SOURCE_JSON__;
function fallback(error) {
  document.documentElement.dataset.islandError = String(error?.message || error);
  const pre = document.createElement('pre');
  pre.textContent = 'JSX rendering failed: ' + String(error?.message || error) + '\\n\\n' + source;
  document.getElementById('root').replaceChildren(pre);
}
// Host Page.Eval first runs code as an expression. A SyntaxError there, on an
// about:blank page, is often sanitized to message "Script error." with no
// Error object and an empty filename. That must not unmount a working tree.
// Real component failures still have an Error, or go through the boundary.
function isSanitizedScriptError(event) {
  return event && !event.error && (event.message === 'Script error.' || !event.filename);
}
window.addEventListener('error', e => {
  if (isSanitizedScriptError(e)) return;
  fallback(e.error || e.message);
});
window.addEventListener('unhandledrejection', e => fallback(e.reason));
class Boundary extends React.Component {
  state = {error: null};
  static getDerivedStateFromError(error) { return {error}; }
  render() { return this.state.error ? React.createElement('pre', null, 'JSX rendering failed: ' + this.state.error.message + '\\n\\n' + source) : this.props.children; }
}
function Counter({initial = 0, label = 'Count'}) {
  const [count, setCount] = React.useState(initial);
  return React.createElement('section', null,
    React.createElement('h3', null, label),
    React.createElement('button', {onClick: () => setCount(count + 1)}, 'Increment'),
    React.createElement('output', {'aria-live': 'polite'}, ' ' + count));
}
function Callout({children, title = 'Note'}) {
  return React.createElement('aside', null, React.createElement('strong', null, title), children);
}
createRoot(document.getElementById('root')).render(React.createElement(Boundary, null,
  React.createElement(Content, {components: {Counter, Callout}})));
