import { useMemo, useState } from "react";

import { connect } from "./client.js";

/**
 * The page, with nothing behind it yet.
 *
 * The sample went with the database. What is left is the part worth keeping:
 * Vite, React, and a grpc-web transport pointed at the server's second
 * listener, which is the wiring that is tedious to get right and dull to write
 * twice.
 *
 * To make it do something:
 *
 *  1. write a service in `proto/<pkg>/`,
 *  2. `./scripts/gen-ui.sh`,
 *  3. `createClient(TheService, transport)` here.
 */
export default function App() {
  // Who the browser says it is. Empty is the anonymous caller, which is a
  // caller like any other -- what one may do is `server.allow_anonymous_reads`.
  const [subject, setSubject] = useState("anna");

  // Rebuilt when `subject` changes, since the credential is a header and a
  // header is decided when the request is made.
  const transport = useMemo(() => connect(() => subject || null), [subject]);
  void transport;

  return (
    <main>
      <h1>go-app</h1>

      <section>
        <h2>who is calling</h2>
        <p className="note">
          Sent as <code>authorization: Plain &lt;subject&gt;</code>, which the
          server believes. Leave it empty to be the anonymous caller.
        </p>
        <input
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
          placeholder="nobody"
          aria-label="subject"
        />
      </section>

      <section>
        <h2>nothing to call</h2>
        <p className="note">
          The server is up and this page can reach it, but there is no service
          on it yet. See <code>docs/EXTENDING.md</code>.
        </p>
      </section>
    </main>
  );
}
