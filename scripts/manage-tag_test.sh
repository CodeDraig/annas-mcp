#!/bin/bash
# Disposable local repositories only; no network or project refs are touched.
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
tag_script="$script_dir/manage-tag.sh"
fixture=$(mktemp -d)
trap 'rm -rf -- "$fixture"' EXIT
real_git=$(command -v git)
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.invalid
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.invalid

git init -q --bare "$fixture/remote"
git init -q "$fixture/work"
cd "$fixture/work"
git remote add origin "$fixture/remote"
mkdir -p internal/version
printf 'v-test\n' > internal/version/version.txt
git add .
git commit -qm old
old_commit=$(git rev-parse HEAD)
git tag -a v-test -m 'original annotated tag'
old_tag=$(git rev-parse refs/tags/v-test)
git push -q origin refs/tags/v-test
git commit --allow-empty -qm replacement
new_commit=$(git rev-parse HEAD)

# Reject replacement: neither the annotated local tag nor remote may change.
cat > "$fixture/remote/hooks/pre-receive" <<'HOOK'
#!/bin/sh
exit 1
HOOK
chmod +x "$fixture/remote/hooks/pre-receive"
if bash "$tag_script" recreate > "$fixture/rejected.log" 2>&1; then echo 'rejected replacement succeeded'; exit 1; fi
test "$(git rev-parse refs/tags/v-test)" = "$old_tag"
test "$(git --git-dir="$fixture/remote" rev-parse refs/tags/v-test)" = "$old_tag"
rm "$fixture/remote/hooks/pre-receive"

# Record updates: successful replacement must be one non-deletion transition.
cat > "$fixture/remote/hooks/pre-receive" <<HOOK
#!/bin/sh
cat > "$fixture/updates"
HOOK
chmod +x "$fixture/remote/hooks/pre-receive"
bash "$tag_script" recreate > "$fixture/success.log" 2>&1
test "$(git rev-parse refs/tags/v-test)" = "$new_commit"
test "$(git --git-dir="$fixture/remote" rev-parse refs/tags/v-test)" = "$new_commit"
test "$(cat "$fixture/updates")" = "$old_tag $new_commit refs/tags/v-test"

# Inject a concurrent remote edit after ls-remote, before the real push.
mkdir "$fixture/bin"
cat > "$fixture/bin/git" <<WRAPPER
#!/bin/bash
if [ "\${1:-}" = push ]; then
 "$real_git" --git-dir="$fixture/remote" update-ref refs/tags/v-test "$old_tag"
fi
exec "$real_git" "\$@"
WRAPPER
chmod +x "$fixture/bin/git"
if PATH="$fixture/bin:$PATH" bash "$tag_script" recreate > "$fixture/race.log" 2>&1; then echo 'stale lease accepted'; exit 1; fi
test "$(git rev-parse refs/tags/v-test)" = "$new_commit"
test "$(git --git-dir="$fixture/remote" rev-parse refs/tags/v-test)" = "$old_tag"

# A concurrent local edit must survive even if remote publication succeeds.
cat > "$fixture/bin/git" <<WRAPPER
#!/bin/bash
if [ "\${1:-}" = push ]; then
 "$real_git" update-ref refs/tags/v-test "$old_tag"
fi
exec "$real_git" "\$@"
WRAPPER
if PATH="$fixture/bin:$PATH" bash "$tag_script" recreate > "$fixture/local-race.log" 2>&1; then echo 'concurrent local edit overwritten'; exit 1; fi
test "$(git rev-parse refs/tags/v-test)" = "$old_tag"
test "$(git --git-dir="$fixture/remote" rev-parse refs/tags/v-test)" = "$new_commit"

# Also support initially absent local and remote tags.
printf 'v-new\n' > internal/version/version.txt
bash "$tag_script" recreate > "$fixture/new.log" 2>&1
test "$(git rev-parse refs/tags/v-new)" = "$new_commit"
test "$(git --git-dir="$fixture/remote" rev-parse refs/tags/v-new)" = "$new_commit"
echo 'manage-tag tests passed (rejection, atomic replacement, stale lease, local race, absent tag)'
