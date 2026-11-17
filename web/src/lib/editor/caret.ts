// Where a caret would be drawn inside a textarea.
//
// A textarea does not say where a given character is on screen. The usual
// trick: copy everything that affects layout (font, padding, border, width,
// wrapping) onto a hidden div, put the text before the caret in it followed
// by a marker span, and read where the span landed. The div wraps lines
// exactly like the textarea, so the span's offset is the caret's.

/** Styles that change where text wraps or how big it is. */
const COPIED = [
  "direction",
  "boxSizing",
  "width",
  "borderTopWidth",
  "borderRightWidth",
  "borderBottomWidth",
  "borderLeftWidth",
  "borderStyle",
  "paddingTop",
  "paddingRight",
  "paddingBottom",
  "paddingLeft",
  "fontStyle",
  "fontVariant",
  "fontWeight",
  "fontStretch",
  "fontSize",
  "fontFamily",
  "lineHeight",
  "letterSpacing",
  "wordSpacing",
  "tabSize",
  "textAlign",
  "textIndent",
  "textTransform",
] as const;

export interface CaretBox {
  /** From the textarea's top-left border corner, ignoring scrolling. */
  top: number;
  left: number;
  height: number;
}

/**
 * The position of a caret before the UTF-16 index `index` of the textarea's
 * value. Subtract the textarea's scrollTop and scrollLeft to get where it is
 * on screen now.
 */
export function caretBox(el: HTMLTextAreaElement, index: number): CaretBox {
  const computed = getComputedStyle(el);
  const mirror = document.createElement("div");
  const style = mirror.style;
  for (const prop of COPIED) style[prop] = computed[prop];
  style.position = "absolute";
  style.visibility = "hidden";
  style.top = "0";
  style.left = "-10000px";
  style.whiteSpace = "pre-wrap";
  style.overflowWrap = "break-word";
  // A visible scrollbar narrows the textarea's text; give the copy one too.
  style.overflowY = el.scrollHeight > el.clientHeight ? "scroll" : "hidden";

  mirror.textContent = el.value.slice(0, index);
  // A zero-width marker at the caret: it has a position without taking
  // space. The rest of the text follows it, so the word the caret is in
  // wraps as it does in the textarea.
  const marker = document.createElement("span");
  marker.textContent = "\u200b";
  mirror.appendChild(marker);
  mirror.appendChild(document.createTextNode(el.value.slice(index)));
  document.body.appendChild(mirror);

  // An inline span's box is the font's glyph area, centred in the line;
  // the caret spans the whole line, as the textarea's own does.
  const lineHeight = parseFloat(computed.lineHeight) || parseFloat(computed.fontSize) * 1.2;
  const lineTop = marker.offsetTop - (lineHeight - marker.offsetHeight) / 2;
  const box = {
    top: lineTop + parseFloat(computed.borderTopWidth),
    left: marker.offsetLeft + parseFloat(computed.borderLeftWidth),
    height: lineHeight,
  };
  mirror.remove();
  return box;
}
