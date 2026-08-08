import { createClient, type Client, type Interceptor } from "@connectrpc/connect";
import { createGrpcWebTransport } from "@connectrpc/connect-web";

import { CoffeeService } from "./gen/go_app/coffee_svc_pb.js";
import { RoasterService } from "./gen/go_app/roaster_svc_pb.js";

/**
 * Where the server's second listener is.
 *
 * The gRPC port is not this one and cannot be: a browser cannot speak the
 * transport gRPC brings, so the server serves grpc-web on a listener of its own
 * and translates it into the same handlers. See `internal/httpx`.
 *
 * The default is **the host this page came from**, on 8080, rather than
 * `localhost`. Those are the same thing until somebody opens the page from
 * another machine -- and then `localhost` is *their* machine, which is the sort
 * of wrong that looks like the server being down.
 *
 * `VITE_API` overrides it, which is what a real deployment does: the API is
 * behind its own name and nothing infers it.
 */
const target =
  import.meta.env?.VITE_API ??
  (typeof location === "undefined"
    ? "http://localhost:8080"
    : `${location.protocol}//${location.hostname}:8080`);

/**
 * says who is calling, in the way `server/auth`'s `plain` handler reads.
 *
 * It believes whatever it is told, which is why it is for development only --
 * the server says so out loud when it starts. A real deployment puts a token
 * here instead (`authorization: Bearer …`), got from whatever issues them, and
 * nothing else about this file changes.
 *
 * Nothing at all is also an answer: a request with no credential is served as
 * the anonymous caller, and what one may do is `server.allow_anonymous_reads`.
 */
function saying(subject: () => string | null): Interceptor {
  return (next) => (req) => {
    const v = subject();
    if (v !== null && v !== "") {
      req.header.set("authorization", `Plain ${v}`);
    }

    return next(req);
  };
}

/**
 * The clients, which are the only thing the app imports.
 *
 * A transport is shared by all of them, since it is the connection and not the
 * service that is expensive.
 */
export function connect(subject: () => string | null): {
  coffee: Client<typeof CoffeeService>;
  roaster: Client<typeof RoasterService>;
} {
  const transport = createGrpcWebTransport({
    baseUrl: target,
    interceptors: [saying(subject)],
  });

  return {
    coffee: createClient(CoffeeService, transport),
    roaster: createClient(RoasterService, transport),
  };
}
