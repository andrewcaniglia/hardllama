import { useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getVaultStatus, reportActivity, type VaultStatus } from "@/api";

declare global {
  interface Window {
    // Called by the app when chats lock because the user left the window.
    __hardllamaLocked?: () => void;
  }
}

const POLL_INTERVAL_MS = 5000;
// How often typing or clicking is reported to the idle lock, at most.
export const ACTIVITY_INTERVAL_MS = 30_000;

export function useVaultStatus() {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["vault"],
    queryFn: getVaultStatus,
    refetchInterval: (q) => (q.state.data?.enabled ? POLL_INTERVAL_MS : false),
  });

  // Show the lock screen as soon as the app locks chats, instead of at the
  // next check, so chats don't stay on screen.
  useEffect(() => {
    const onLocked = () => {
      queryClient.setQueryData<VaultStatus>(["vault"], (old) =>
        old?.enabled ? { ...old, locked: true } : old,
      );
      queryClient.invalidateQueries({ queryKey: ["vault"] });
    };
    window.__hardllamaLocked = onLocked;
    return () => {
      if (window.__hardllamaLocked === onLocked) {
        delete window.__hardllamaLocked;
      }
    };
  }, [queryClient]);

  const locked = query.data?.locked;
  useReportActivity(query.data?.enabled === true && locked === false);
  const wasLocked = useRef(locked);
  useEffect(() => {
    if (locked) {
      // Decrypted chats must not stay in memory behind the lock screen.
      queryClient.removeQueries({
        predicate: (q) => q.queryKey[0] === "chat" || q.queryKey[0] === "chats",
      });
      // Encrypted settings read as empty while locked.
      queryClient.invalidateQueries({ queryKey: ["settings"] });
    } else if (wasLocked.current) {
      queryClient.invalidateQueries({ queryKey: ["chats"] });
      queryClient.invalidateQueries({ queryKey: ["settings"] });
    }
    wasLocked.current = locked;
  }, [locked, queryClient]);

  return query;
}

// useReportActivity tells the idle lock the user is still here while they
// type, click or scroll, which otherwise doesn't reach the app until a
// message is sent.
function useReportActivity(unlocked: boolean) {
  useEffect(() => {
    if (!unlocked) {
      return;
    }
    let last = 0;
    const onActivity = () => {
      const now = Date.now();
      if (now - last < ACTIVITY_INTERVAL_MS) {
        return;
      }
      last = now;
      reportActivity().catch(() => {});
    };
    const events = ["keydown", "pointerdown", "wheel"] as const;
    for (const e of events) {
      window.addEventListener(e, onActivity, { passive: true });
    }
    return () => {
      for (const e of events) {
        window.removeEventListener(e, onActivity);
      }
    };
  }, [unlocked]);
}
