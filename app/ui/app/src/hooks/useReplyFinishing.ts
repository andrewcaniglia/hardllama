import { useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getReplyFinishing } from "@/api";

// A reply keeps being written if chats lock, and the window isn't sent it.
// useReplyFinishing reports whether a chat's reply is still finishing after
// an unlock, and keeps the chat up to date until it's done.
export function useReplyFinishing(chatId: string): boolean {
  const queryClient = useQueryClient();
  const { data, dataUpdatedAt } = useQuery({
    queryKey: ["chat", chatId, "finishing"],
    queryFn: () => getReplyFinishing(chatId),
    enabled: !!chatId,
    refetchInterval: (q) => (q.state.data?.finishing ? 1000 : false),
  });
  const finishing = !!data?.finishing;

  const wasFinishing = useRef(false);
  useEffect(() => {
    if (finishing || wasFinishing.current) {
      // Show what's been added, and once more when it's done.
      queryClient.invalidateQueries({
        queryKey: ["chat", chatId],
        exact: true,
      });
    }
    wasFinishing.current = finishing;
  }, [chatId, finishing, dataUpdatedAt, queryClient]);

  return finishing;
}
