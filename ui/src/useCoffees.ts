import { useEffect, useState } from "react";
import { Code, ConnectError, type Client } from "@connectrpc/connect";

import type { Coffee } from "./gen/go_app/coffee_pb.js";
import type { CoffeeService } from "./gen/go_app/coffee_svc_pb.js";

export type Watched = {
  coffees: Coffee[];
  error: string | null;
  /** what the last message said changed it, or null for the first one. */
  action: string | null;
};

/**
 * Watches every Coffee there is, and answers with what there is.
 *
 * This is the whole of what a client of `Watch` has to do, and it is short for
 * a reason worth knowing: **what arrives is state and never a delta.** An item
 * carries the Coffee as it is now, so the client keeps the last thing it was
 * told about each id and replaces it. There is no version to compare, no
 * ordering to preserve, and no backlog to replay -- a message that never
 * arrived is one that did not need to, since the next one about that Coffee
 * carries the whole of it.
 *
 * A removal is said by **absence**: an item with no `value` is a Coffee that is
 * no longer one this caller may see, so it is dropped. Nothing distinguishes
 * "erased" from "no longer visible", and nothing here needs to.
 *
 * The first message is everything that matches now, so there is no List call
 * before this and no race between the two. A Coffee may arrive twice -- once in
 * that first message and once as a change that landed while it was being read
 * -- which is harmless for the same reason the rest of it is.
 */
export function useCoffees(client: Client<typeof CoffeeService>, subject: string): Watched {
  const [coffees, setCoffees] = useState<Map<string, Coffee>>(new Map());
  const [error, setError] = useState<string | null>(null);
  const [action, setAction] = useState<string | null>(null);

  useEffect(() => {
    // A stream of its own per caller: who is asking is a header, and a header
    // is sent when the request is made.
    const stop = new AbortController();

    setCoffees(new Map());
    setError(null);
    setAction(null);

    void (async () => {
      try {
        for await (const res of client.watch({}, { signal: stop.signal })) {
          setCoffees((was) => {
            const now = new Map(was);
            for (const item of res.items) {
              const id = hex(item.id);
              if (item.value === undefined) {
                now.delete(id);
              } else {
                now.set(id, item.value);
              }
            }

            return now;
          });

          const last = res.items.at(-1)?.action;
          setAction(last === undefined || last === "" ? null : last);
        }
      } catch (err) {
        if (stop.signal.aborted) {
          return;
        }

        setError(say(err));
      }
    })();

    return () => stop.abort();
  }, [client, subject]);

  return {
    // Newest last, which is the order `List` answers in and so the order the
    // first message arrived in.
    coffees: [...coffees.values()].sort((a, b) => at(a) - at(b)),
    error,
    action,
  };
}

function at(v: Coffee): number {
  return Number(v.dateCreated?.seconds ?? 0n);
}

/** hex is a key a Map can hold, since two `Uint8Array`s are never equal. */
export function hex(v: Uint8Array): string {
  return [...v].map((b) => b.toString(16).padStart(2, "0")).join("");
}

/**
 * say is what went wrong, in the words the server used.
 *
 * The code is kept rather than flattened into a sentence, because the code is
 * what says what to do about it: `Unauthenticated` is fixed by saying who you
 * are, and `PermissionDenied` is not.
 */
export function say(err: unknown): string {
  if (err instanceof ConnectError) {
    return `${Code[err.code]}: ${err.rawMessage}`;
  }

  return String(err);
}
