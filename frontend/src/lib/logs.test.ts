import { describe, expect, it } from "vitest";
import { applyLogUpdate } from "./logs";

describe("applyLogUpdate", () => {
  it("appends lines within one daemon generation", () => {
    expect(applyLogUpdate([{ seq: 1, line: "old" }], [{ seq: 2, line: "next" }])).toEqual([
      { seq: 1, line: "old" },
      { seq: 2, line: "next" },
    ]);
  });

  it("replaces lines after a daemon generation reset", () => {
    expect(applyLogUpdate([{ seq: 9, line: "old daemon" }], [{ seq: 1, line: "new daemon" }], true)).toEqual([
      { seq: 1, line: "new daemon" },
    ]);
  });
});
