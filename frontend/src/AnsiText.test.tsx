import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { AnsiText } from "./AnsiText";

describe("AnsiText", () => {
  it("renders standard and bright ANSI colors without exposing escape sequences", () => {
    const html = renderToStaticMarkup(<AnsiText>{"normal \x1b[90mdim time\x1b[0m done"}</AnsiText>);

    expect(html).toContain("normal ");
    expect(html).toContain("color:#4c566a");
    expect(html).toContain("dim time");
    expect(html).toContain(" done");
    expect(html).not.toContain("[90m");
  });

  it("handles the visible escape notation returned by some log producers", () => {
    const html = renderToStaticMarkup(<AnsiText>{"␛[2J␛[H␛[31mfailed␛[0m"}</AnsiText>);

    expect(html).toContain("color:#bf616a");
    expect(html).toContain("failed");
    expect(html).not.toContain("␛");
    expect(html).not.toContain("[2J");
  });

  it("renders 256-color and true-color sequences", () => {
    const html = renderToStaticMarkup(<AnsiText>{"\x1b[38;5;214mone\x1b[48;2;1;2;3mtwo"}</AnsiText>);

    expect(html).toContain("color:rgb(255, 175, 0)");
    expect(html).toContain("background-color:rgb(1, 2, 3)");
  });
});
