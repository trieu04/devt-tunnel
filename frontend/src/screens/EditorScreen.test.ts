import { describe, expect, it } from "vitest";
import { TunnelType } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";
import { detectCommandType, normalizeCommandDashes } from "./EditorScreen";

describe("normalizeCommandDashes", () => {
  it("restores command flags changed by macOS Smart Dashes", () => {
    expect(normalizeCommandDashes("cloudflared tunnel —url http://localhost:3000")).toBe(
      "cloudflared tunnel --url http://localhost:3000",
    );
    expect(normalizeCommandDashes("ngrok http 3000 –domain example.com")).toBe("ngrok http 3000 --domain example.com");
  });
});

describe("detectCommandType", () => {
  it.each([
    ["ssh", TunnelType.TypeSSH],
    ["$ ssh ", TunnelType.TypeSSH],
    ["/usr/local/bin/cloudflared", TunnelType.TypeCloudflared],
    ["ngrok", TunnelType.TypeNgrok],
  ])("detects %s before the command is complete", (command, expected) => {
    expect(detectCommandType(command)).toBe(expected);
  });

  it("waits until the executable name is complete", () => {
    expect(detectCommandType("ss")).toBeUndefined();
    expect(detectCommandType("cloudflare")).toBeUndefined();
  });
});
