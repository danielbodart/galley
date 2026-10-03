#!/usr/bin/env bash
# Seven made-up questions, a second apart, shaped like the ones galley's
# callers ask -- an egress broker's asker (a request, an SSH command, a
# recording's connection), a grant approver's diff, and sudo's askpass -- so
# they stack in one window. Nothing is requested: each answer is only
# printed, as exit status and stdout, once all seven are answered.
#
#   examples/stack.sh            # galley from PATH
#   GALLEY=./result/bin/galley examples/stack.sh
set -u
galley=${GALLEY:-galley}
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

ask() { # name, then galley's arguments
  local name=$1; shift
  { "$galley" "$@" > "$out/$name.out"; echo $? > "$out/$name.status"; } &
  sleep 1
}

ask request-delete --question --no-markup --title="Allow this request?" \
  --ok-label=Allow --cancel-label=Refuse --width=640 \
  --text="/home/alice/Projects/shop

DELETE api.github.com/repos/alice/shop/git/refs/heads/old-experiment

Delete a reference (guarded, git)
Deletes the provided reference."

ask request-dns --question --no-markup --title="Allow this request?" \
  --ok-label=Allow --cancel-label=Refuse --width=640 \
  --text="/home/alice/Projects/infra

POST api.cloudflare.com/client/v4/zones/2f1c9a/dns_records
{\"type\":\"A\",\"name\":\"demo.example.com\",\"content\":\"203.0.113.7\",\"ttl\":300}

Create DNS Record (write, DNS Records for a Zone)
Create a new DNS record for a zone."

ask ssh-restart --question --no-markup --title="Run this command?" \
  --ok-label=Allow --cancel-label=Refuse --width=640 \
  --text="/home/alice/Projects/infra

alice@server (192.0.2.10): sudo systemctl restart caddy

Restart a systemd unit (write, services)"

ask record-registry --question --no-markup --title="Connect to this name?" \
  --ok-label=Allow --cancel-label=Refuse --extra-button=Ask --width=640 \
  --text="Recording -- your answer is written to the grant. Allow and Ask let it through now; Ask records it as one to be asked about each time, Refuse as refused.

/home/alice/Projects/shop

registry.npmjs.org (104.16.30.34)

A connection to a name the tier's network does not allow. What it sends is not seen: only the name and port are decided."

ask record-nas --question --no-markup --title="Connect to this name?" \
  --ok-label=Allow --cancel-label=Refuse --extra-button=Ask --width=640 \
  --text="Recording -- your answer is written to the grant. Allow and Ask let it through now; Ask records it as one to be asked about each time, Refuse as refused.

/home/alice/Projects/shop

nas.lan (192.168.1.20)

A connection to a name the tier's network does not allow. What it sends is not seen: only the name and port are decided.
Its address is on the local network."

cat > "$out/diff" <<'EOF'
What this checkout's chase.jsonc asks for has changed since you last
approved it.

/home/alice/Projects/shop

--- approved
+++ proposed
@@ -3,6 +3,12 @@
   "apps": {
     "docker": {
-      "images": ["postgres:18"],
+      "images": ["postgres:18", "redis:8"],
       "ports": [64320]
     }
   },
+  "network": {
+    "allow": ["registry.npmjs.org"],
+    "lan": [{"name": "nas.lan", "ports": [80]}]
+  },
+  "seccomp": {"allow": ["io_uring_setup", "io_uring_enter"]}
 }
EOF
ask approve-grant --text-info --title="Approve this change?" \
  --ok-label=Approve --cancel-label=Refuse --width=720 --height=520 \
  --filename="$out/diff"

ask sudo --entry --hide-text --title="Authentication Required" \
  --text="Authenticate to run as root:

    systemctl restart caddy

Password for alice:"

wait
for f in "$out"/*.status; do
  name=$(basename "$f" .status)
  answer=$(cat "$out/$name.out")
  # A hidden entry's text is a password: say only that one came back.
  if [ "$name" = sudo ] && [ -n "$answer" ]; then answer="(${#answer} characters)"; fi
  printf '%-16s exit %s  %s\n' "$name" "$(cat "$f")" "$answer"
done
