import { useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getVaultStatus } from "@/api";

const POLL_INTERVAL_MS = 5000;

export function useVaultStatus() {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["vault"],
    queryFn: getVaultStatus,
    refetchInterval: (q) => (q.state.data?.enabled ? POLL_INTERVAL_MS : false),
  });

  const locked = query.data?.locked;
  const wasLocked = useRef(locked);
  useEffect(() => {
    if (locked) {
      // Decrypted chats must not stay in memory behind the lock screen.
      queryClient.removeQueries({
        predicate: (q) => q.queryKey[0] === "chat" || q.queryKey[0] === "chats",
      });
    } else if (wasLocked.current) {
      queryClient.invalidateQueries({ queryKey: ["chats"] });
    }
    wasLocked.current = locked;
  }, [locked, queryClient]);

  return query;
}
