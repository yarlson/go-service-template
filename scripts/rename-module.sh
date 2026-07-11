#!/bin/sh
set -eu

new_module=${1:-}
case "$new_module" in
  ""|*[!A-Za-z0-9._~/-]*)
    echo "usage: $0 <valid-go-module-path>" >&2
    exit 1
    ;;
esac

old_module=$(go list -m)
if [ "$old_module" = "$new_module" ]; then
  exit 0
fi

find . -type f -name '*.go' -not -path './.git/*' | while IFS= read -r file; do
  temporary_file="${file}.tmp"
  sed "s|${old_module}|${new_module}|g" "$file" > "$temporary_file"
  mv "$temporary_file" "$file"
done

go mod edit -module="$new_module"
go mod tidy
echo "renamed module from $old_module to $new_module"
