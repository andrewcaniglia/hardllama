import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ClockIcon } from "@heroicons/react/24/outline";
import { getLockDelay, setLockDelay } from "@/api";
import { Description, Field, Label } from "@/components/ui/fieldset";
import { Slider } from "@/components/ui/slider";

export const LOCK_DELAY_OPTIONS = [
  { value: 0, label: "Instant" },
  { value: 5, label: "5 sec" },
  { value: 15, label: "15 sec" },
  { value: 30, label: "30 sec" },
  { value: 60, label: "1 min" },
];

// LockDelaySettings chooses how long chats stay unlocked after the user
// switches to another app or closes the window.
export function LockDelaySettings() {
  const queryClient = useQueryClient();
  const { data } = useQuery({ queryKey: ["lockDelay"], queryFn: getLockDelay });
  const [error, setError] = useState("");

  const handleChange = async (seconds: number) => {
    setError("");
    try {
      queryClient.setQueryData(["lockDelay"], await setLockDelay(seconds));
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to change lock timing",
      );
      queryClient.invalidateQueries({ queryKey: ["lockDelay"] });
    }
  };

  return (
    <Field>
      <div className="flex items-start space-x-3">
        <ClockIcon className="mt-1 h-5 w-5 flex-shrink-0 text-black dark:text-neutral-100" />
        <div className="w-full">
          <Label>Lock after leaving</Label>
          <Description>
            How long chats stay unlocked after you switch to another app or
            close the window. Coming back sooner keeps them unlocked.
          </Description>
          <div className="mt-3">
            <Slider
              value={data?.seconds ?? 5}
              onChange={handleChange}
              disabled={!data}
              options={LOCK_DELAY_OPTIONS}
            />
          </div>
          {error && (
            <p
              role="alert"
              className="mt-1 text-sm text-red-600 dark:text-red-400"
            >
              {error}
            </p>
          )}
        </div>
      </div>
    </Field>
  );
}
