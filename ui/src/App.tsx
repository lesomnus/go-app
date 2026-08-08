import { useEffect, useMemo, useState } from "react";

import { connect } from "./client.js";
import { hex, say, useCoffees } from "./useCoffees.js";
import type { Roaster } from "./gen/go_app/roaster_pb.js";

export default function App() {
  // Who the browser says it is. Empty is the anonymous caller, which is a
  // caller like any other -- what one may do is `server.allow_anonymous_reads`.
  const [subject, setSubject] = useState("anna");

  // Rebuilt when `subject` changes, since the credential is a header and a
  // header is decided when the request is made.
  const client = useMemo(() => connect(() => subject || null), [subject]);

  const [roasters, setRoasters] = useState<Roaster[]>([]);
  const [error, setError] = useState<string | null>(null);
  const watched = useCoffees(client.coffee, subject);

  // Roasters are read once rather than watched, so that both halves of the API
  // are shown: this is the `List` a page would call, and the Coffees below are
  // the `Watch` it would keep open.
  const readRoasters = async () => {
    try {
      const res = await client.roaster.list({});
      setRoasters(res.items);
      setError(null);
    } catch (err) {
      setError(say(err));
    }
  };

  useEffect(() => {
    void readRoasters();
  }, [client]);

  const run = async (f: () => Promise<unknown>) => {
    try {
      await f();
      setError(null);
    } catch (err) {
      setError(say(err));
    }
  };

  return (
    <main>
      <h1>go-app</h1>

      <section>
        <h2>who is calling</h2>
        <p className="note">
          Sent as <code>authorization: Plain &lt;subject&gt;</code>, which the server
          believes. Leave it empty to be the anonymous caller.
        </p>
        <input
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
          placeholder="nobody"
          aria-label="subject"
        />
      </section>

      {error !== null && <p className="error">{error}</p>}

      <section>
        <h2>roasters</h2>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const alias = new FormData(e.currentTarget).get("alias");
            void run(async () => {
              await client.roaster.add({ alias: String(alias ?? "") });
              await readRoasters();
            });
            e.currentTarget.reset();
          }}
        >
          <input name="alias" placeholder="beans" aria-label="roaster alias" />
          <button type="submit">add</button>
        </form>

        <ul>
          {roasters.map((v) => (
            <li key={hex(v.id)}>
              <code>{v.alias}</code>
              <button
                onClick={() =>
                  void run(async () => {
                    await client.roaster.erase({ key: { case: "id", value: v.id } });
                    await readRoasters();
                  })
                }
              >
                erase
              </button>
            </li>
          ))}
        </ul>
        {roasters.length === 0 && <p className="note">none yet</p>}
      </section>

      <section>
        <h2>coffees</h2>
        <p className="note">
          Watched, not polled. What arrives is the row as it is now, so this list
          is what the server has rather than what this page remembers asking for.
          {watched.action !== null && (
            <>
              {" "}
              Last change: <code>{watched.action}</code>.
            </>
          )}
        </p>
        {watched.error !== null && <p className="error">{watched.error}</p>}

        <form
          onSubmit={(e) => {
            e.preventDefault();
            const f = new FormData(e.currentTarget);
            const roaster = String(f.get("roaster") ?? "");
            void run(() =>
              client.coffee.add({
                roaster: { key: { case: "alias", value: roaster } },
                alias: String(f.get("alias") ?? ""),
              }),
            );
            e.currentTarget.reset();
          }}
        >
          <input name="roaster" placeholder="beans" aria-label="roaster" />
          <input name="alias" placeholder="ethiopia" aria-label="coffee alias" />
          <button type="submit">add</button>
        </form>

        <ul>
          {watched.coffees.map((v) => (
            <li key={hex(v.id)}>
              <code>
                {v.roaster?.alias ?? "?"}/{v.alias}
              </code>
              <button
                onClick={() =>
                  void run(() => client.coffee.erase({ key: { case: "id", value: v.id } }))
                }
              >
                erase
              </button>
            </li>
          ))}
        </ul>
        {watched.coffees.length === 0 && <p className="note">none yet</p>}
      </section>
    </main>
  );
}
