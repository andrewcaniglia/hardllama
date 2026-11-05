import { act, create } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { VaultLockScreen } from "./VaultLockScreen";

const mocks = vi.hoisted(() => ({ unlockVault: vi.fn() }));

vi.mock("@/api", () => ({ unlockVault: mocks.unlockVault }));

function render(queryClient: QueryClient) {
  let renderer: ReturnType<typeof create>;
  act(() => {
    renderer = create(
      <QueryClientProvider client={queryClient}>
        <VaultLockScreen />
      </QueryClientProvider>,
    );
  });
  return renderer!;
}

async function submit(renderer: ReturnType<typeof create>, password: string) {
  act(() => {
    renderer.root
      .findByProps({ "aria-label": "Password", type: "password" })
      .props.onChange({ target: { value: password } });
  });
  await act(async () => {
    await renderer.root
      .findByType("form")
      .props.onSubmit({ preventDefault: () => {} });
  });
}

describe("VaultLockScreen", () => {
  beforeEach(() => {
    mocks.unlockVault.mockReset();
  });

  it("shows an error and stays locked on a wrong password", async () => {
    mocks.unlockVault.mockRejectedValue(new Error("incorrect password"));
    const queryClient = new QueryClient();
    const renderer = render(queryClient);

    await submit(renderer, "wrong");

    expect(renderer.root.findByProps({ role: "alert" }).children).toEqual([
      "incorrect password",
    ]);
    expect(queryClient.getQueryData(["vault"])).toBeUndefined();
  });

  it("publishes the unlocked status", async () => {
    const status = { enabled: true, locked: false };
    mocks.unlockVault.mockResolvedValue(status);
    const queryClient = new QueryClient();

    await submit(render(queryClient), "correct");

    expect(mocks.unlockVault).toHaveBeenCalledWith("correct");
    expect(queryClient.getQueryData(["vault"])).toEqual(status);
  });
});
