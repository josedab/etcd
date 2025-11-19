#!/usr/bin/env bash

# Verify security scanning configuration
# This script validates that the security scanning setup is correctly configured

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "Verifying security scanning configuration..."

# Check that security-scan.yaml workflow exists
WORKFLOW_FILE="${ROOT_DIR}/.github/workflows/security-scan.yaml"
if [[ ! -f "${WORKFLOW_FILE}" ]]; then
    echo "ERROR: Security scan workflow not found at ${WORKFLOW_FILE}"
    exit 1
fi
echo "  - Security scan workflow exists"

# Check that the workflow is valid YAML
if command -v yamllint &> /dev/null; then
    if ! yamllint -d relaxed "${WORKFLOW_FILE}" &> /dev/null; then
        echo "WARNING: Workflow may have YAML issues. Run yamllint for details."
    else
        echo "  - Workflow YAML syntax is valid"
    fi
else
    echo "  - Skipping YAML validation (yamllint not installed)"
fi

# Check that govulncheck is available or can be installed
if command -v govulncheck &> /dev/null; then
    echo "  - govulncheck is installed"
    govulncheck --version 2>/dev/null || true
else
    echo "  - govulncheck not installed (will be installed by make vuln-check)"
fi

# Check that Makefile has required targets
if grep -q "^vuln-check:" "${ROOT_DIR}/Makefile"; then
    echo "  - Makefile has vuln-check target"
else
    echo "ERROR: Makefile missing vuln-check target"
    exit 1
fi

if grep -q "^security:" "${ROOT_DIR}/Makefile"; then
    echo "  - Makefile has security target"
else
    echo "ERROR: Makefile missing security target"
    exit 1
fi

# Check PR template has security checklist
PR_TEMPLATE="${ROOT_DIR}/.github/PULL_REQUEST_TEMPLATE.md"
if [[ -f "${PR_TEMPLATE}" ]] && grep -q "Security Checklist" "${PR_TEMPLATE}"; then
    echo "  - PR template has security checklist"
else
    echo "WARNING: PR template missing security checklist"
fi

# Check dependabot has security configuration
DEPENDABOT="${ROOT_DIR}/.github/dependabot.yml"
if [[ -f "${DEPENDABOT}" ]] && grep -q "security" "${DEPENDABOT}"; then
    echo "  - Dependabot has security configuration"
else
    echo "WARNING: Dependabot missing security configuration"
fi

echo ""
echo "Security scanning verification completed successfully!"
