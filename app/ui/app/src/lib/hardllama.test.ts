import { describe, expect, it } from "vitest";
import { OLLAMA_ACCOUNTS, THIRD_PARTY_APPS } from "./hardllama";

describe("HardLlama features", () => {
  it("leaves out other companies' apps and Ollama's cloud", () => {
    expect(THIRD_PARTY_APPS).toBe(false);
    expect(OLLAMA_ACCOUNTS).toBe(false);
  });
});
