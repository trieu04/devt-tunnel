import type { CSSProperties, ReactNode } from "react";

type AnsiStyle = CSSProperties & { color?: string; backgroundColor?: string };

const basicColors = [
  "#2e3440",
  "#bf616a",
  "#a3be8c",
  "#ebcb8b",
  "#81a1c1",
  "#b48ead",
  "#88c0d0",
  "#d8dee9",
  "#4c566a",
  "#d06f79",
  "#b1d196",
  "#f0d399",
  "#8fbcdf",
  "#c895bf",
  "#93ccdc",
  "#eceff4",
];

function ansiColor(index: number): string | undefined {
  if (index < 0 || index > 255) return undefined;
  if (index < 16) return basicColors[index];
  if (index >= 232) {
    const level = 8 + (index - 232) * 10;
    return `rgb(${level}, ${level}, ${level})`;
  }
  const value = (part: number) => (part === 0 ? 0 : 55 + part * 40);
  const offset = index - 16;
  return `rgb(${value(Math.floor(offset / 36))}, ${value(Math.floor((offset % 36) / 6))}, ${value(offset % 6)})`;
}

function applyCodes(current: AnsiStyle, codes: number[]): AnsiStyle {
  let style = { ...current };
  for (let index = 0; index < codes.length; index++) {
    const code = codes[index];
    if (code === 0) style = {};
    else if (code === 1) style.fontWeight = 700;
    else if (code === 2) style.opacity = 0.65;
    else if (code === 3) style.fontStyle = "italic";
    else if (code === 4) style.textDecoration = "underline";
    else if (code === 22) {
      delete style.fontWeight;
      delete style.opacity;
    } else if (code === 23) delete style.fontStyle;
    else if (code === 24) delete style.textDecoration;
    else if (code >= 30 && code <= 37) style.color = ansiColor(code - 30);
    else if (code >= 90 && code <= 97) style.color = ansiColor(code - 90 + 8);
    else if (code === 39) delete style.color;
    else if (code >= 40 && code <= 47) style.backgroundColor = ansiColor(code - 40);
    else if (code >= 100 && code <= 107) style.backgroundColor = ansiColor(code - 100 + 8);
    else if (code === 49) delete style.backgroundColor;
    else if ((code === 38 || code === 48) && codes[index + 1] === 5) {
      const color = ansiColor(codes[index + 2]);
      if (color) style[code === 38 ? "color" : "backgroundColor"] = color;
      index += 2;
    } else if ((code === 38 || code === 48) && codes[index + 1] === 2) {
      const rgb = codes.slice(index + 2, index + 5);
      if (rgb.length === 3 && rgb.every((value) => value >= 0 && value <= 255)) {
        style[code === 38 ? "color" : "backgroundColor"] = `rgb(${rgb.join(", ")})`;
      }
      index += 4;
    }
  }
  return style;
}

export function renderAnsi(text: string): ReactNode[] {
  const parts: ReactNode[] = [];
  const pattern = /(?:\x1b|␛)\[([0-9;?]*)([ -/]*)([@-~])/g;
  let style: AnsiStyle = {};
  let start = 0;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(text))) {
    if (match.index > start) {
      const value = text.slice(start, match.index);
      parts.push(
        Object.keys(style).length ? (
          <span key={parts.length} style={{ ...style }}>
            {value}
          </span>
        ) : (
          value
        ),
      );
    }
    if (match[3] === "m") style = applyCodes(style, (match[1] || "0").split(";").map(Number));
    start = pattern.lastIndex;
  }
  if (start < text.length) {
    const value = text.slice(start);
    parts.push(
      Object.keys(style).length ? (
        <span key={parts.length} style={{ ...style }}>
          {value}
        </span>
      ) : (
        value
      ),
    );
  }
  return parts;
}

export function AnsiText({ children }: { children: string }) {
  return <>{renderAnsi(children)}</>;
}
