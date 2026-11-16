import { AppSidebar } from "@/components/AppSidebar";
import { ConnectAppsScreen } from "@/components/Onboarding";
import { SidebarLayout } from "@/components/layout/layout";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { THIRD_PARTY_APPS } from "@/lib/hardllama";

export const Route = createFileRoute("/connect")({
  beforeLoad: () => {
    if (!THIRD_PARTY_APPS) {
      throw redirect({ to: "/" });
    }
  },
  component: ConnectRoute,
});

function ConnectRoute() {
  return (
    <SidebarLayout title="Apps" sidebar={<AppSidebar current="apps" />}>
      <ConnectAppsScreen />
    </SidebarLayout>
  );
}
