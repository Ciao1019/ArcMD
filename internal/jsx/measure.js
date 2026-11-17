// Must be a single expression: MyGo Page.Eval first wraps the code in
// await(\n<code>\n). A top-level return is a SyntaxError there and, in
// WKWebView, can also fire a sanitized window "Script error." event.
// Do not use document.documentElement.scrollHeight or document.body.scrollHeight
// as the height; a 240 DIP WebView viewport makes those max(content, viewport)
// and cannot report a shorter island.
(function () {
  var measure = function () {
    var root = document.getElementById("root");
    var pad = 0;
    if (document.body && typeof getComputedStyle === "function") {
      var cs = getComputedStyle(document.body);
      pad = (parseFloat(cs.paddingTop) || 0) + (parseFloat(cs.paddingBottom) || 0);
    }
    var content = 0;
    if (root) {
      var rootRect = root.getBoundingClientRect();
      content = Math.max(root.offsetHeight || 0, rootRect.height || 0);
      var children = root.children || [];
      for (var i = 0; i < children.length; i++) {
        var child = children[i];
        var rect = child.getBoundingClientRect();
        var top = rect.top - (rootRect.top || 0);
        content = Math.max(
          content,
          top + (rect.height || 0),
          (child.offsetTop || 0) + (child.offsetHeight || 0)
        );
      }
    }
    var height = Math.round(content + pad);
    try {
      document.documentElement.dataset.islandHeight = String(height);
    } catch (e) {}
    return height;
  };
  if (typeof window !== "undefined" && !window.__arcmdMeasureIsland) {
    window.__arcmdMeasureIsland = measure;
    var publish = function () {
      try {
        measure();
      } catch (e) {}
    };
    var attach = function (node) {
      try {
        if (!node || node.__arcmdHeightObserved) {
          return;
        }
        node.__arcmdHeightObserved = true;
        if (typeof ResizeObserver === "function") {
          if (!window.__arcmdHeightRO) {
            window.__arcmdHeightRO = new ResizeObserver(publish);
          }
          window.__arcmdHeightRO.observe(node);
        }
        if (typeof MutationObserver === "function") {
          new MutationObserver(publish).observe(node, {
            subtree: true,
            childList: true,
            characterData: true,
            attributes: true
          });
        }
      } catch (e) {}
    };
    try {
      attach(document.getElementById("root"));
      if (!document.getElementById("root") && document.documentElement && typeof MutationObserver === "function") {
        new MutationObserver(function () {
          try {
            attach(document.getElementById("root"));
            publish();
          } catch (e) {}
        }).observe(document.documentElement, { subtree: true, childList: true });
      }
    } catch (e) {}
  }
  var root = document.getElementById("root");
  return {
    height: measure(),
    ready: !!(root && root.childElementCount)
  };
})()
