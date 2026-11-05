import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import { getSettings } from "@/api";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCloudStatus } from "@/hooks/useCloudStatus";
import { preloadChatData } from "@/lib/chatPreload";
import { preventPageSelectAll } from "@/lib/keyboard";
import { useEffect } from "react";
import { VaultLockScreen } from "@/components/VaultLockScreen";
import { VaultSetupScreen } from "@/components/VaultSetupScreen";
import { useVaultStatus } from "@/hooks/useVault";

function RootComponent() {
  const queryClient = useQueryClient();

  useEffect(() => {
    document.addEventListener("keydown", preventPageSelectAll);
    return () => document.removeEventListener("keydown", preventPageSelectAll);
  }, []);

  useEffect(() => {
    void preloadChatData(queryClient);
  }, [queryClient]);

  // This hook ensures settings are fetched on app startup
  useQuery({
    queryKey: ["settings"],
    queryFn: getSettings,
  });
  // Fetch cloud status on startup (best-effort)
  useCloudStatus();

  const { data: vault, isPending } = useVaultStatus();

  // Nothing is usable until a password exists and has been entered.
  if (isPending) {
    return null;
  }
  if (vault?.locked) {
    return <VaultLockScreen />;
  }
  if (vault && !vault.enabled) {
    return <VaultSetupScreen />;
  }

  return (
    <div>
      <Outlet />
    </div>
  );
}

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  component: RootComponent,
});
