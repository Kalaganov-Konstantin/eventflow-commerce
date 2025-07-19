# Development Guide

This guide provides instructions for setting up the development environment for EventFlow Commerce.

## Prerequisites
- Docker
- Docker Compose
- Go
- Python
- Makefile

## Getting Started
1. Clone the repository.
2. Run `make up` to start all services.
3. See the main `README.md` for service endpoints.

## Pre-commit Hooks

This project uses pre-commit hooks to ensure code quality and consistency before committing changes. The hooks automatically check for formatting issues, linting errors, and other common problems.

### Setup

To use the pre-commit hooks, you need to install the `pre-commit` tool and then set up the hooks in your local repository.

1.  **Install pre-commit:**
    ```bash
    pip install pre-commit
    ```

2.  **Install the git hooks:**
    ```bash
    pre-commit install
    ```

Now, the hooks will run automatically on `git commit`. You can also run them manually at any time with:
```bash
pre-commit run --all-files
```
