import { act, create } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  CustomInstructionsSettings,
  MAX_CUSTOM_INSTRUCTIONS,
} from "./CustomInstructionsSettings";

const mocks = vi.hoisted(() => ({
  getCustomInstructions: vi.fn(),
  setCustomInstructions: vi.fn(),
}));

vi.mock("@/api", () => mocks);

type Renderer = ReturnType<typeof create>;

async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

async function render() {
  let renderer: Renderer;
  await act(async () => {
    renderer = create(
      <QueryClientProvider client={new QueryClient()}>
        <CustomInstructionsSettings />
      </QueryClientProvider>,
    );
  });
  await flush();
  return renderer!;
}

const textarea = (r: Renderer) =>
  r.root.findByProps({ "aria-label": "Custom instructions" });
const save = (r: Renderer) =>
  r.root.findAll(
    (n) =>
      typeof n.type !== "string" &&
      n.props.onClick !== undefined &&
      n.props.children === "Save",
  )[0];
const type = (r: Renderer, value: string) =>
  act(() => textarea(r).props.onChange({ target: { value } }));

describe("CustomInstructionsSettings", () => {
  beforeEach(() => {
    mocks.getCustomInstructions.mockReset();
    mocks.setCustomInstructions.mockReset();
  });

  it("shows and saves the instructions", async () => {
    mocks.getCustomInstructions.mockResolvedValue({ instructions: "be brief" });
    mocks.setCustomInstructions.mockImplementation(async (instructions) => ({
      instructions,
    }));
    const r = await render();
    expect(textarea(r).props.value).toBe("be brief");
    // Nothing to save until it changes.
    expect(save(r).props.disabled).toBe(true);

    type(r, "be brief and kind");
    await act(async () => {
      await save(r).props.onClick();
    });

    expect(mocks.setCustomInstructions).toHaveBeenCalledWith(
      "be brief and kind",
    );
    expect(JSON.stringify(r.toJSON())).toContain("Saved.");
    expect(save(r).props.disabled).toBe(true);
  });

  it("won't save instructions that are too long", async () => {
    mocks.getCustomInstructions.mockResolvedValue({ instructions: "" });
    const r = await render();
    type(r, "x".repeat(MAX_CUSTOM_INSTRUCTIONS + 1));
    expect(save(r).props.disabled).toBe(true);
    expect(JSON.stringify(r.toJSON())).toContain("At most 20,000");
  });

  it("shows why saving failed", async () => {
    mocks.getCustomInstructions.mockResolvedValue({ instructions: "" });
    mocks.setCustomInstructions.mockRejectedValue(
      new Error("chats are locked"),
    );
    const r = await render();
    type(r, "be brief");
    await act(async () => {
      await save(r).props.onClick();
    });
    expect(JSON.stringify(r.toJSON())).toContain("chats are locked");
  });
});
