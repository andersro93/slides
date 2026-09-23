import { describe, expect, test } from "bun:test";
import { codeFromPath, isValidCode, normalizeCode } from "../src/lib/code";
import { buildConfig } from "../src/lib/deck-config";
import { firstLine, remoteUrl } from "../src/lib/protocol";

describe("normalizeCode", () => {
  test.each([
    ["kubecon26", "kubecon26"],
    ["  KubeCon26 ", "kubecon26"],
    ["mint orbit falcon", "mint-orbit-falcon"],
    ["/abc/", "abc"],
    ["https://slides.ros-nett.com/Talk-1/#/3", "talk-1"],
    ["http://localhost:3000/abc/_remote", "abc"],
    ["abc?x=1", "abc"],
  ])("%p → %p", (input, want) => {
    expect(normalizeCode(input)).toBe(want);
  });
});

describe("isValidCode", () => {
  test.each([
    ["abc", true],
    ["mint-orbit-falcon-42", true],
    ["ab", false],
    ["-abc", false],
    ["abc-", false],
    ["_app", false],
    ["a.b", false],
    ["ABC", false],
    ["a".repeat(64), true],
    ["a".repeat(65), false],
  ])("%p → %p", (code, want) => {
    expect(isValidCode(code)).toBe(want);
  });
});

test("codeFromPath", () => {
  expect(codeFromPath("/abc/")).toBe("abc");
  expect(codeFromPath("/ABC/_remote")).toBe("abc");
  expect(codeFromPath("/")).toBe("");
  expect(codeFromPath("/%E0%A4%A/")).toBe("%e0%a4%a");
});

test("buildConfig merges deck options but never plugins", () => {
  const plugin = () => ({ id: "x", init() {} });
  const cfg = buildConfig(
    { reveal: { transition: "fade", hash: false, plugins: ["evil"] } },
    [plugin],
  );
  expect(cfg.transition).toBe("fade");
  expect(cfg.hash).toBe(false);
  expect(cfg.slideNumber).toBe("c/t");
  expect(cfg.plugins).toEqual([plugin]);
});

test("remoteUrl keeps the token in the fragment", () => {
  expect(remoteUrl("https://s.example", "abc", "TOK")).toBe(
    "https://s.example/abc/_remote#TOK",
  );
});

test("firstLine", () => {
  expect(firstLine("\n  Hello\nworld")).toBe("Hello");
  expect(firstLine(null)).toBe("");
  expect(firstLine("x".repeat(100), 10)).toBe(`${"x".repeat(9)}…`);
});
