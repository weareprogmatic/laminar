# Contributing to Laminar

Thank you for considering contributing to Laminar! We appreciate your time and effort.

## Code of Conduct

By participating in this project, you agree to maintain a respectful and inclusive environment for all contributors.

## How to Contribute

### Reporting Bugs

If you find a bug, please open an issue on [GitHub](https://github.com/weareprogmatic/laminar/issues) with:

- A clear description of the bug
- Steps to reproduce
- Expected vs. actual behavior
- Your environment (OS, Go version, Laminar version)
- Relevant logs or error messages

### Suggesting Features

Feature suggestions are welcome! Please open an issue with:

- Clear description of the feature
- Use case / motivation
- Proposed implementation (optional)

### Development Setup

1. **Fork and clone the repository**:

```bash
git clone https://github.com/YOUR_USERNAME/laminar.git
cd laminar
```

2. **Install dependencies**:

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Install goimports
go install golang.org/x/tools/cmd/goimports@latest
```

3. **Build the project**:

```bash
make build
```

4. **Run tests**:

```bash
make test
```

### Development Workflow

#### Branch Naming

- `feature/description` - New features
- `fix/description` - Bug fixes
- `docs/description` - Documentation updates
- `refactor/description` - Code refactoring
- `test/description` - Test improvements

#### Making Changes

1. **Create a branch**:

```bash
git checkout -b feature/my-new-feature
```

2. **Write tests first** (TDD):

```bash
# Add tests to *_test.go files
# Run tests to verify they fail
make test
```

3. **Implement your changes**:

- Follow Go best practices and "Effective Go" principles
- Keep functions small and focused
- Use descriptive variable names
- Add godoc comments for exported types and functions
- Handle errors explicitly (no silent failures)
- Use `fmt.Errorf("context: %w", err)` for error wrapping

4. **Run quality checks**:

```bash
make fmt      # Format code
make lint     # Run linter
make test     # Run tests
make coverage # Check coverage (must be ≥ 90%)
```

5. **Commit your changes**:

```bash
git add .
git commit -m "feat: add new feature description"
```

Use [Conventional Commits](https://www.conventionalcommits.org/):
- `feat:` New features
- `fix:` Bug fixes
- `docs:` Documentation changes
- `refactor:` Code refactoring
- `test:` Test changes
- `chore:` Build/tooling changes

6. **Push and create a pull request**:

```bash
git push origin feature/my-new-feature
```

Then open a PR on GitHub.

### Code Style Guidelines

#### Go Standards

- **Formatting**: All code must pass `gofmt` and `goimports`
- **Errors**: Always handle errors; never ignore them
- **Error Wrapping**: Use `%w` format verb for error wrapping
- **Naming**: 
  - Interfaces end in `"er"` (e.g., `Runner`, `Parser`)
  - Short but descriptive variable names (e.g., `cfg` for config, `srv` for server)
- **Documentation**: 
  - Every package must have a `doc.go` file
  - All exported types, functions, and constants must have godoc comments
  - Comments must start with the entity name (e.g., `// Parser parses...`)
- **Concurrency**: Document goroutine lifecycles and use channels for orchestration
- **Panic**: Never use `panic()` in production code

#### Testing Requirements

- **Test Coverage**: Minimum 90% coverage for all packages
- **Test Files**: Use `*_test.go` naming
- **Table-Driven Tests**: Prefer table-driven tests for multiple cases
- **Test Names**: Use descriptive test names (e.g., `TestLoadInvalidJSON`)
- **TDD**: Write failing tests before implementation
- **Race Detection**: Tests must pass with `-race` flag

#### Code Organization

```
cmd/laminar/          # Main application entrypoint
internal/config/      # Configuration loading and validation
internal/server/      # HTTP server and middleware
internal/runner/      # Process execution
internal/payload/     # Lambda payload mapping
internal/response/    # Lambda response parsing
internal/version/     # Version information
examples/             # Example Lambda binaries
```

### Pull Request Process

1. **Ensure all checks pass**:
   - Tests pass with `-race`
   - Coverage ≥ 90%
   - Linter shows no errors
   - Code is formatted

2. **Update documentation**:
   - Update README.md if adding features
   - Add godoc comments
   - Update CHANGELOG.md

3. **Write a clear PR description**:
   - What does this PR do?
   - Why is this change needed?
   - How was it tested?
   - Any breaking changes?

4. **Request review**:
   - PRs require at least one approval
   - Address review feedback promptly

5. **Squash and merge**:
   - PRs are squashed into a single commit
   - Use a descriptive commit message

### Testing

#### Unit Tests

```bash
make test
```

#### Coverage

```bash
make coverage
# View in browser:
go tool cover -html=artifacts/coverage.out
```

#### Integration Tests

Run the full build and test the example:

```bash
make build
./artifacts/laminar &
LAMINAR_PID=$!
sleep 2
curl http://localhost:8081
kill $LAMINAR_PID
```

### Release Process

Releases are created via GitHub Actions when version tags are pushed:

1. **Update version**: Ensure CHANGELOG or version bumps are committed

2. **Create and push tag**:

```bash
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

3. **GitHub Actions will automatically**:
   - Generate changelog from commits since last tag
   - Create a GitHub Release with installation instructions
   - Make version available via `go install github.com/weareprogmatic/laminar/cmd/laminar@v1.0.0`

**Note**: No binaries are built - users install via `go install`.

### Getting Help

- Open an issue for questions
- Join discussions on GitHub Discussions
- Tag maintainers in PRs if you need help

### License

By contributing, you agree that your contributions will be licensed under the same dual license as the project (MIT / Apache 2.0).

---

Thank you for contributing to Laminar! 🚀
