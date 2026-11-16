import { act, create } from "react-test-renderer";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useDraft } from "./useDraft";

const mocks = vi.hoisted(() => ({
  saveDraft: vi.fn(),
  takeDraft: vi.fn(),
}));

vi.mock("@/api", () => mocks);

const restore = vi.fn();

function Form(props: { chatId: string; content: string; skip?: boolean }) {
  useDraft({ skip: false, restore, ...props });
  return null;
}

async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

async function render(props: Parameters<typeof Form>[0]) {
  let renderer: ReturnType<typeof create>;
  await act(async () => {
    renderer = create(<Form {...props} />);
  });
  await flush();
  return renderer!;
}

describe("useDraft", () => {
  beforeEach(() => {
    restore.mockReset();
    mocks.saveDraft.mockReset().mockResolvedValue(undefined);
    mocks.takeDraft.mockReset().mockResolvedValue({ content: "" });
  });

  it("saves what was typed when the form goes away, such as on lock", async () => {
    const renderer = await render({ chatId: "c1", content: "half a prompt" });
    act(() => renderer.unmount());
    expect(mocks.saveDraft).toHaveBeenCalledWith("c1", "half a prompt");
  });

  it("puts the draft back when the form returns", async () => {
    mocks.takeDraft.mockResolvedValue({ content: "half a prompt" });
    await render({ chatId: "c1", content: "" });
    expect(mocks.takeDraft).toHaveBeenCalledWith("c1");
    expect(restore).toHaveBeenCalledWith("half a prompt");
  });

  it("saves under the chat it was typed in when switching chats", async () => {
    const renderer = await render({ chatId: "c1", content: "for c1" });
    await act(async () => {
      renderer.update(<Form chatId="c2" content="for c1" />);
    });
    expect(mocks.saveDraft).toHaveBeenCalledWith("c1", "for c1");
    expect(mocks.takeDraft).toHaveBeenLastCalledWith("c2");
  });

  it("keeps nothing for private chats or empty forms", async () => {
    const priv = await render({ chatId: "c1", content: "secret", skip: true });
    act(() => priv.unmount());
    const empty = await render({ chatId: "c2", content: "  " });
    act(() => empty.unmount());
    expect(mocks.saveDraft).not.toHaveBeenCalled();
  });

  it("doesn't restore a draft into a private chat", async () => {
    mocks.takeDraft.mockResolvedValue({ content: "half a prompt" });
    await render({ chatId: "c1", content: "", skip: true });
    expect(restore).not.toHaveBeenCalled();
  });
});
