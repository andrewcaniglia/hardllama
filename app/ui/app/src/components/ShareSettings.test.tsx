import { act, create } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ShareSettings } from "./ShareSettings";

const mocks = vi.hoisted(() => ({
  getShare: vi.fn(),
  setShare: vi.fn(),
  startPairing: vi.fn(),
  removeShareDevice: vi.fn(),
}));

vi.mock("@/api", () => mocks);

type Renderer = ReturnType<typeof create>;

const off = {
  enabled: false,
  running: false,
  port: "11443",
  addresses: ["192.168.1.20"],
  devices: [],
};
const on = { ...off, enabled: true, running: true };
const device = { id: "d1", name: "Mac, paired Nov 15, 2026", created_at: "" };

async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

async function render() {
  const queryClient = new QueryClient();
  let renderer: Renderer;
  await act(async () => {
    renderer = create(
      <QueryClientProvider client={queryClient}>
        <ShareSettings />
      </QueryClientProvider>,
    );
  });
  await flush();
  return { renderer: renderer!, queryClient };
}

const button = (renderer: Renderer, label: string) =>
  renderer.root
    .findAll(
      (n) =>
        typeof n.type !== "string" &&
        n.props.onClick !== undefined &&
        n.props.children === label,
    )
    .at(0);

const text = (renderer: Renderer) => JSON.stringify(renderer.toJSON());

describe("ShareSettings", () => {
  beforeEach(() => {
    for (const mock of Object.values(mocks)) mock.mockReset();
  });

  it("turns sharing on and shows where to connect", async () => {
    mocks.getShare.mockResolvedValue(off);
    mocks.setShare.mockResolvedValue(on);
    const { renderer } = await render();
    expect(button(renderer, "Pair a device")).toBeUndefined();

    await act(async () => {
      await renderer.root
        .findAll((n) => n.props.checked === false && n.props.onChange)[0]
        .props.onChange(true);
    });

    expect(mocks.setShare).toHaveBeenCalledWith(true);
    expect(text(renderer)).toContain("192.168.1.20");
    expect(text(renderer)).toContain("11443");
    expect(button(renderer, "Pair a device")).toBeDefined();
  });

  it("shows a pairing code until a device pairs", async () => {
    mocks.getShare.mockResolvedValue(on);
    mocks.startPairing.mockResolvedValue({
      code: "ABCD-EFGH-IJKL-MNOP",
      expires_at: new Date(Date.now() + 300000).toISOString(),
    });
    const { renderer, queryClient } = await render();

    await act(async () => {
      await button(renderer, "Pair a device")!.props.onClick();
    });
    expect(
      renderer.root.findByProps({ "aria-label": "Pairing code" }).children,
    ).toEqual(["ABCD-EFGH-IJKL-MNOP"]);

    // The device pairs.
    await act(async () => {
      queryClient.setQueryData(["share"], { ...on, devices: [device] });
    });
    await flush();

    expect(
      renderer.root.findAllByProps({ "aria-label": "Pairing code" }),
    ).toHaveLength(0);
    expect(text(renderer)).toContain("Mac, paired Nov 15, 2026");
  });

  it("removes a device", async () => {
    mocks.getShare.mockResolvedValue({ ...on, devices: [device] });
    mocks.removeShareDevice.mockResolvedValue(on);
    const { renderer } = await render();

    await act(async () => {
      await button(renderer, "Remove")!.props.onClick();
    });

    expect(mocks.removeShareDevice).toHaveBeenCalledWith("d1");
    expect(text(renderer)).not.toContain("Mac, paired");
  });

  it("shows errors", async () => {
    mocks.getShare.mockResolvedValue(off);
    mocks.setShare.mockRejectedValue(new Error("chats are locked"));
    const { renderer } = await render();

    await act(async () => {
      await renderer.root
        .findAll((n) => n.props.checked === false && n.props.onChange)[0]
        .props.onChange(true);
    });

    expect(renderer.root.findByProps({ role: "alert" }).children).toEqual([
      "chats are locked",
    ]);
  });
});
