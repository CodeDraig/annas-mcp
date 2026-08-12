#!/bin/bash

# Networked healthcheck for annas-cli search operations.

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "====================================="
echo "  Anna's Archive CLI Healthcheck"
echo "====================================="
echo ""

TESTS_PASSED=0
TESTS_FAILED=0

echo -e "${YELLOW}[TEST 1]${NC} Testing book search with term 'crypto'..."
if BOOK_RESULT=$(go run ./cmd/annas-cli book-search "crypto" 2>&1); then
    if echo "$BOOK_RESULT" | grep -Fq '"books":[{'; then
        echo -e "${GREEN}✓ PASSED${NC} - Book search returned results"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        echo -e "${RED}✗ FAILED${NC} - Book search returned no results"
        echo "$BOOK_RESULT"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
else
    BOOK_EXIT_CODE=$?
    echo -e "${RED}✗ FAILED${NC} - Book search command failed with exit code $BOOK_EXIT_CODE"
    echo "$BOOK_RESULT"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi
echo ""

echo -e "${YELLOW}[TEST 2]${NC} Testing article search with DOI '10.48550/arXiv.1706.03762'..."
if ARTICLE_RESULT=$(go run ./cmd/annas-cli article-search "10.48550/arXiv.1706.03762" 2>&1); then
    if echo "$ARTICLE_RESULT" | grep -Fq '"articles":[{'; then
        echo -e "${GREEN}✓ PASSED${NC} - Article search returned paper details"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        echo -e "${RED}✗ FAILED${NC} - Article search returned no paper details"
        echo "$ARTICLE_RESULT"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
else
    ARTICLE_EXIT_CODE=$?
    echo -e "${RED}✗ FAILED${NC} - Article search command failed with exit code $ARTICLE_EXIT_CODE"
    echo "$ARTICLE_RESULT"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi
echo ""

echo "====================================="
echo "  Test Summary"
echo "====================================="
echo -e "Total tests: $((TESTS_PASSED + TESTS_FAILED))"
echo -e "${GREEN}Passed: $TESTS_PASSED${NC}"
echo -e "${RED}Failed: $TESTS_FAILED${NC}"
echo ""

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}All healthchecks passed!${NC}"
    exit 0
fi

echo -e "${RED}Some healthchecks failed!${NC}"
exit 1
