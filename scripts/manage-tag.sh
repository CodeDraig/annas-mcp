#!/bin/bash

if [ $# -eq 0 ]; then
    echo "Usage: $0 <add|delete|recreate>"
    echo "  add      - Create and push a new tag"
    echo "  delete   - Delete the tag locally and from GitHub"
    echo "  recreate - Delete and recreate the tag"
    exit 1
fi

ACTION=$1

if [ "$ACTION" != "add" ] && [ "$ACTION" != "delete" ] && [ "$ACTION" != "recreate" ]; then
    echo "Error: Invalid action. Use 'add', 'delete', or 'recreate'"
    exit 1
fi

if [ ! -f "internal/version/version.txt" ]; then
    echo "Error: internal/version/version.txt file not found"
    exit 1
fi

VERSION=$(cat internal/version/version.txt | tr -d '[:space:]')

if [ -z "$VERSION" ]; then
    echo "Error: version.txt is empty"
    exit 1
fi

case "$ACTION" in
    "add")
        echo "Creating and pushing tag: $VERSION"
        
        git tag "$VERSION"
        
        if [ $? -eq 0 ]; then
            echo "Tag $VERSION created successfully"
            
            git push origin "$VERSION"
            
            if [ $? -eq 0 ]; then
                echo "Tag $VERSION pushed to GitHub successfully"
            else
                echo "Error: Failed to push tag $VERSION to GitHub"
                exit 1
            fi
        else
            echo "Error: Failed to create tag $VERSION"
            exit 1
        fi
        ;;
        
    "delete")
        echo "Deleting tag: $VERSION"
        
        git tag -d "$VERSION"
        
        if [ $? -eq 0 ]; then
            echo "Local tag $VERSION deleted successfully"
            
            git push origin --delete "$VERSION"
            
            if [ $? -eq 0 ]; then
                echo "Remote tag $VERSION deleted from GitHub successfully"
            else
                echo "Error: Failed to delete remote tag $VERSION from GitHub"
                exit 1
            fi
        else
            echo "Error: Failed to delete local tag $VERSION"
            exit 1
        fi
        ;;
        
    "recreate")
        # Prepare the replacement without changing either published or local tags.
        tag_ref="refs/tags/$VERSION"
        git check-ref-format "$tag_ref" || exit 1
        new_oid=$(git rev-parse --verify 'HEAD^{commit}') || exit 1
        local_oid=$(git rev-parse --verify "$tag_ref" 2>/dev/null) || local_oid=""
        remote_record=$(git ls-remote --refs origin "$tag_ref") || exit 1
        remote_oid=${remote_record%%[[:space:]]*}
        echo "Replacing $tag_ref: remote ${remote_oid:-absent}, local ${local_oid:-absent}, new $new_oid"

        # The explicit lease also protects an absent tag from concurrent creation.
        # A rejected push leaves both old refs intact; there is no deletion gap.
        if ! git push --force-with-lease="$tag_ref:$remote_oid" origin "$new_oid:$tag_ref"; then
            echo "Error: Replacement push failed; local tag preserved. Verify remote state before retrying." >&2
            exit 1
        fi
        # Compare-and-swap preserves a concurrent local edit after publication.
        if ! git update-ref "$tag_ref" "$new_oid" "$local_oid"; then
            echo "Error: Remote now points to $new_oid, but local tag changed concurrently; local state preserved." >&2
            exit 1
        fi
        echo "Tag $VERSION recreated successfully"
        ;;
esac
