import { act, create } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { VaultSetupScreen } from "./VaultSetupScreen";

const mocks = vi.hoisted(() => ({ setupVault: vi.fn() }));

vi.mock("@/api", () => ({ setupVault: mocks.setupVault }));

function render(queryClient: QueryClient) {
  let renderer: ReturnType<typeof create>;
  act(() => {
    renderer = create(
      <QueryClientProvider client={queryClient}>
        <VaultSetupScreen />
      </QueryClientProvider>,
    );
  });
  return renderer!;
}

async function submit(
  renderer: ReturnType<typeof create>,
  password: string,
  confirmation: string,
) {
  const type = (label: string, value: string) =>
    act(() => {
      renderer.root
        .findByProps({ "aria-label": label, type: "password" })
        .props.onChange({ target: { value } });
    });
  type("Password", password);
  type("Confirm password", confirmation);
  await act(async () => {
    await renderer.root
      .findByType("form")
      .props.onSubmit({ preventDefault: () => {} });
  });
}

describe("VaultSetupScreen", () => {
  beforeEach(() => {
    mocks.setupVault.mockReset();
  });

  it("refuses passwords that don't match", async () => {
    const renderer = render(new QueryClient());

    await submit(renderer, "correct horse", "correct hose");

    expect(mocks.setupVault).not.toHaveBeenCalled();
    expect(renderer.root.findByProps({ role: "alert" }).children).toEqual([
      "Passwords don't match",
    ]);
  });

  it("shows server errors and stays on the screen", async () => {
    mocks.setupVault.mockRejectedValue(new Error("setup failed"));
    const queryClient = new QueryClient();
    const renderer = render(queryClient);

    await submit(renderer, "correct horse", "correct horse");

    expect(renderer.root.findByProps({ role: "alert" }).children).toEqual([
      "setup failed",
    ]);
    expect(queryClient.getQueryData(["vault"])).toBeUndefined();
  });

  it("publishes the enabled status", async () => {
    const status = { enabled: true, locked: false };
    mocks.setupVault.mockResolvedValue(status);
    const queryClient = new QueryClient();

    await submit(render(queryClient), "correct horse", "correct horse");

    expect(mocks.setupVault).toHaveBeenCalledWith("correct horse");
    expect(queryClient.getQueryData(["vault"])).toEqual(status);
  });
});
