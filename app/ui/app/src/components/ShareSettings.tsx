import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ServerStackIcon } from "@heroicons/react/24/outline";
import {
  getShare,
  removeShareDevice,
  setShare,
  startPairing,
  type PairingCode,
  type ShareStatus,
} from "@/api";
import { Button } from "@/components/ui/button";
import { Description, Field, Label } from "@/components/ui/fieldset";
import { Switch } from "@/components/ui/switch";

export function ShareSettings() {
  const queryClient = useQueryClient();
  const [pairing, setPairing] = useState<PairingCode | null>(null);
  // While a code is showing, check for the device it pairs.
  const { data } = useQuery({
    queryKey: ["share"],
    queryFn: getShare,
    refetchInterval: pairing ? 2000 : false,
  });
  const devicesWhenShown = useRef(0);
  const deviceCount = data?.devices.length ?? 0;
  useEffect(() => {
    if (pairing && deviceCount > devicesWhenShown.current) {
      setPairing(null);
    }
  }, [pairing, deviceCount]);
  const [error, setError] = useState("");
  const [isSaving, setIsSaving] = useState(false);

  const run = async (action: () => Promise<void>) => {
    setIsSaving(true);
    setError("");
    try {
      await action();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong");
    } finally {
      setIsSaving(false);
    }
  };

  const save = (status: ShareStatus) =>
    queryClient.setQueryData(["share"], status);

  const handleToggle = (enabled: boolean) =>
    run(async () => {
      save(await setShare(enabled));
      setPairing(null);
    });

  const handlePair = () =>
    run(async () => {
      devicesWhenShown.current = deviceCount;
      setPairing(await startPairing());
    });

  const handleRemove = (id: string) =>
    run(async () => {
      save(await removeShareDevice(id));
    });

  const enabled = data?.enabled ?? false;
  const expires = pairing
    ? new Date(pairing.expires_at).toLocaleTimeString([], {
        hour: "numeric",
        minute: "2-digit",
      })
    : "";

  return (
    <Field>
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start space-x-3 flex-1">
          <ServerStackIcon className="mt-1 h-5 w-5 flex-shrink-0 text-black dark:text-neutral-100" />
          <div>
            <Label>Share models with my devices (Experimental)</Label>
            <Description>
              Let devices you pair run models on this computer. They can't see
              your chats. Sharing keeps working while HardLlama is locked.
            </Description>
          </div>
        </div>
        <div className="flex-shrink-0">
          <Switch
            checked={enabled}
            disabled={isSaving || !data}
            onChange={handleToggle}
          />
        </div>
      </div>

      {enabled && data && (
        <div className="mt-3 ml-8 space-y-3 text-sm text-neutral-600 dark:text-neutral-300">
          <p>
            {data.addresses.length > 0
              ? `Other devices connect to ${data.addresses.join(" or ")}, port ${data.port}.`
              : `Other devices connect to this computer's address, port ${data.port}.`}
          </p>

          {pairing ? (
            <div>
              <p>Enter this code on the other device. It works once.</p>
              <p
                aria-label="Pairing code"
                className="my-1 font-mono text-lg tracking-wider text-neutral-900 dark:text-white"
              >
                {pairing.code}
              </p>
              <p className="text-neutral-500 dark:text-neutral-400">
                Expires at {expires}.
              </p>
            </div>
          ) : (
            <Button type="button" disabled={isSaving} onClick={handlePair}>
              Pair a device
            </Button>
          )}

          {data.devices.length > 0 && (
            <ul className="space-y-1">
              {data.devices.map((device) => (
                <li
                  key={device.id}
                  className="flex items-center justify-between gap-2"
                >
                  <span>{device.name}</span>
                  <Button
                    type="button"
                    plain
                    disabled={isSaving}
                    onClick={() => handleRemove(device.id)}
                  >
                    Remove
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {error && (
        <p
          role="alert"
          className="mt-1 ml-8 text-sm text-red-600 dark:text-red-400"
        >
          {error}
        </p>
      )}
    </Field>
  );
}
