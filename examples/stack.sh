#!/usr/bin/env bash
# Seven made-up questions, a second apart, shaped like the ones galley's
# callers ask -- an egress broker's asker (a request, an SSH command, a
# recording's connection), a grant approver's diff, and sudo's askpass --
# and then one of each of zenity's other dialogs -- a list, a check list, a
# form, a calendar, a scale, a password, a colour, a file to save, a combo,
# a progress bar fed as it goes and a notification -- so they stack in one
# window. Nothing is requested: each answer is only printed, as exit status
# and stdout, once all are answered. The notification returns at once, and
# its entry stays until it is dismissed.
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

ask list --list --title="Which container should start?" --width=560 \
  --text="/home/alice/Projects/shop" \
  --column=Image --column=Port --column=Status \
  postgres:18 64320 stopped redis:8 63790 stopped caddy:2 8080 running

ask checklist --list --checklist --title="Which grants should be kept?" --width=560 \
  --column=Keep --column=Grant --column=Last-used --separator=, \
  TRUE "registry.npmjs.org" "today" FALSE "nas.lan:80" "3 weeks ago" TRUE "api.github.com" "yesterday"

ask forms --forms --title="New sandbox" --text="Describe the sandbox to create." \
  --add-entry=Name --add-combo=Tier --combo-values="trusted|ops|untrusted" \
  --add-calendar=Expires --forms-date-format=%Y-%m-%d --add-multiline-entry=Notes --separator=";"

ask calendar --calendar --title="Expire this grant on" --day=31 --month=12 --year=2026 --date-format=%Y-%m-%d

ask scale --scale --title="How many workers?" --text="Concurrent builds" --min-value=1 --max-value=16 --value=4

ask password --password --username --title="Log in to the registry"

ask colour --color-selection --title="Pick the workspace's colour" --color="#3584e4"

ask save --file-selection --save --title="Save the audit log" \
  --filename="$HOME/audit.log" --file-filter="Logs | *.log *.txt"

ask combo --entry --title="Which branch?" --text="Branch to deploy:" --entry-text=main develop release/2.0

{ for i in 10 25 40 55 70 85 100; do echo "# Copying layer $((i / 15 + 1)) of 7"; echo "$i"; sleep 2; done | \
    "$galley" --progress --title="Pulling postgres:18" --time-remaining > "$out/progress.out"
  echo $? > "$out/progress.status"; } &
sleep 1

ask notification --notification --text="Build finished\nshop: 214 tests passed in 3m 12s" --icon=emblem-ok-symbolic

wait
for f in "$out"/*.status; do
  name=$(basename "$f" .status)
  answer=$(cat "$out/$name.out")
  # A hidden entry's text is a password: say only that one came back.
  if [ "$name" = sudo ] && [ -n "$answer" ]; then answer="(${#answer} characters)"; fi
  if [ "$name" = password ] && [ -n "$answer" ]; then pw=${answer#*|}; answer="${answer%%|*}|(${#pw} characters)"; fi
  printf '%-16s exit %s  %s\n' "$name" "$(cat "$f")" "$answer"
done
