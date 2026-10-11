# Contributing to LLM-Based Intelligent Autoscaling System

Thank you for your interest in contributing to our project! Please read the following guidelines to help make the contribution process effective and enjoyable for everyone involved.

## How to Contribute

### Reporting Bugs
Before submitting a bug report, please check if it has already been reported by searching the issue tracker. If you're unable to find an open issue addressing the problem, open a new issue using the bug report template.

### Suggesting Features
Feature requests are welcome! Please open an issue using the feature request template and provide as much detail as possible about the use case and expected behavior.

### Submitting Changes
We welcome pull requests! Here's how to get started:

1. Fork the repository on GitHub
2. Clone your fork locally: `git clone https://github.com/<your-username>/llm-assisted-predictive-autoscaling.git`
3. Create a new branch for your changes: `git checkout -b feature-or-fix-name`
4. Make your changes, following the coding standards below
5. Add tests for your changes
6. Ensure all tests pass: `make test`
7. Commit your changes: `git commit -m "Descriptive commit message"`
8. Push to your branch: `git push origin feature-or-fix-name`
9. Open a pull request against the `main` branch

## Coding Standards

### Go Code
- Follow the [Effective Go](https://golang.org/doc/effective_guide.html) guidelines
- Use `gofmt` to format your code (`make fmt`)
- Use `golangci-lint` for linting (`make lint`)
- Write clear, concise comments
- Keep functions focused and small
- Handle errors appropriately

### Python Code
- Follow [PEP 8](https://pep8.org/) guidelines
- Use `black` for code formatting
- Use `flake8` for linting
- Write docstrings for all public functions and classes
- Keep functions focused and small
- Handle exceptions appropriately

### Documentation
- Keep documentation up-to-date with code changes
- Write clear, concise explanations
- Use examples where helpful
- Follow the existing documentation style

## Development Setup

### Prerequisites
- Go 1.19+
- Python 3.9+
- Docker and Docker Compose (for full-stack testing)
- Make (for running build commands)

### Getting Started
1. Clone the repository: `git clone https://github.com/mhkarimi1383/llm-assisted-predictive-autoscaling.git`
2. Change to the project directory: `cd llm-assisted-predictive-autoscaling`
3. Install dependencies:
   - Go: `go mod download`
   - Python: `pip install -r requirements.txt` (if applicable)
4. Run the application: `make run`
   - Or start the full environment: `make compose-up`

### Running Tests
- Run all tests: `make test`
- Run unit tests only: `make test-unit`
- Run integration tests: `make test-integration`

### Code Formatting
- Format Go code: `make fmt`
- Lint Go code: `make lint`

## Community

Please note that this project is released with a Contributor Code of Conduct. By participating in this project you agree to abide by its terms. Please see [CODE_OF_CONDUCT.md] for details.

## Licensing

By contributing to this project, you agree that your contributions will be licensed under the GNU Affero General Public License v3.0 (AGPL-3.0).