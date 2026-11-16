import { act, create } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ModelServerSettings } from "./ModelServerSettings";

const mocks = vi.hoisted(() => ({
  getModelServer: vi.fn(),
  pairModelServer: vi.fn(),
  unpairModelServer: vi.fn(),
}));

vi.mock("@/api", () => mocks);

type Renderer = ReturnType<typeof create>;

async function render() {
  const queryClient = new QueryClient();
  let renderer: Renderer;
  await act(async () => {
    renderer = create(
      <QueryClientProvider client={queryClient}>
        <ModelServerSettings />
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  return { renderer: renderer!, queryClient };
}

function type(renderer: Renderer, label: string, value: string) {
  act(() => {
    renderer.root
      .findByProps({ "aria-label": label })
      .props.onChange({ target: { value } });
  });
}

const button = (renderer: Renderer, text: string) =>
  renderer.root
    .findAll(
      (n) =>
        typeof n.type !== "string" &&
        n.props.onClick !== undefined &&
        n.props.children === text,
    )
    .at(0);

const text = (renderer: Renderer) => JSON.stringify(renderer.toJSON());

describe("ModelServerSettings", () => {
  beforeEach(() => {
    for (const mock of Object.values(mocks)) mock.mockReset();
  });

  it("pairs with an address and code", async () => {
    mocks.getModelServer.mockResolvedValue({ address: "" });
    mocks.pairModelServer.mockResolvedValue({ address: "192.168.1.20:11443" });
    const { renderer, queryClient } = await render();

    type(renderer, "Other computer's address", "192.168.1.20");
    type(renderer, "Pairing code", "abcd-efgh-ijkl-mnop");
    await act(async () => {
      await renderer.root
        .findByType("form")
        .props.onSubmit({ preventDefault: () => {} });
    });

    expect(mocks.pairModelServer).toHaveBeenCalledWith(
      "192.168.1.20",
      "abcd-efgh-ijkl-mnop",
    );
    expect(queryClient.getQueryData(["modelServer"])).toEqual({
      address: "192.168.1.20:11443",
    });
    expect(text(renderer)).toContain("192.168.1.20:11443");
    expect(renderer.root.findAllByType("form")).toHaveLength(0);
  });

  it("shows why pairing failed and keeps what was typed", async () => {
    mocks.getModelServer.mockResolvedValue({ address: "" });
    mocks.pairModelServer.mockRejectedValue(
      new Error("the pairing code is wrong or has expired"),
    );
    const { renderer } = await render();

    type(renderer, "Other computer's address", "192.168.1.20");
    type(renderer, "Pairing code", "wrong");
    await act(async () => {
      await renderer.root
        .findByType("form")
        .props.onSubmit({ preventDefault: () => {} });
    });

    expect(renderer.root.findByProps({ role: "alert" }).children).toEqual([
      "the pairing code is wrong or has expired",
    ]);
    expect(
      renderer.root.findByProps({ "aria-label": "Other computer's address" })
        .props.value,
    ).toBe("192.168.1.20");
  });

  it("goes back to this computer", async () => {
    mocks.getModelServer.mockResolvedValue({ address: "192.168.1.20:11443" });
    mocks.unpairModelServer.mockResolvedValue({ address: "" });
    const { renderer } = await render();
    expect(text(renderer)).toContain("192.168.1.20:11443");

    await act(async () => {
      await button(renderer, "Use this computer")!.props.onClick();
    });

    expect(mocks.unpairModelServer).toHaveBeenCalled();
    expect(renderer.root.findAllByType("form")).toHaveLength(1);
  });
});
