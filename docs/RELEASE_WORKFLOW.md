# Release Workflow & Setup Guide

## 1. Branch Structure
- **`develop`**: Main development branch. All features are merged here first via Pull Request.
- **`master`**: Stable release branch. Protected. Only merged from `develop`.

## 2. GitHub Configuration (Required Once)

As a repository administrator, you must configure these settings manually in the GitHub interface:

### Branch Protection Rules
1. Go to **Settings** -> **Branches**.
2. Click **Add branch protection rule**.
3. **Branch name pattern**: `master`
4. Check **Require a pull request before merging**.
   - [Optional] *Require approvals*: 1
5. Check **Require status checks to pass before merging**.
   - Search for **Test & Build** (this is the name of our CI job).
6. Check **Do not allow bypassing the above settings**.
7. Click **Create** / **Save**.

### Actions Permissions (For Releases)
1. Go to **Settings** -> **Actions** -> **General**.
2. Scroll to **Workflow permissions**.
3. Ensure **Read and write permissions** is selected.
4. Click **Save**.

## 3. How to Release

1. **Merge Code**: Ensure `develop` has the features you want to release.
2. **Create Release PR**: Create a PR from `develop` to `master`.
3. **Merge PR**: Merge the PR into `master`.
4. **Tag Release**:
   - Create and push a tag starting with `v` (e.g., `v0.1.0`).
   ```bash
   git checkout master
   git pull
   git tag v0.1.0
   git push origin v0.1.0
   ```
5. **Wait**: The GitHub Action `Release` workflow will automatically:
   - Run tests.
   - Build binaries for Linux/macOS (amd64/arm64).
   - Create a GitHub Release page with the changelog and attached binaries.
