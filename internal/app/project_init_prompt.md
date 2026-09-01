Initialize the repository in the current working directory with these two optional targets:

- `AGENTS.md`: a concise, repository-specific contributor guide.
- `.snow/config.json`: a minimal project-local Snow configuration containing exactly an empty JSON object.

Follow this workflow exactly:

1. Check whether `AGENTS.md` and `.snow/config.json` already exist in the current working directory before making any changes. Treat the targets independently. Never modify, replace, append to, rename, or delete either existing target.
2. If `AGENTS.md` does not exist, inspect the repository with read and search tools before writing. Use only facts verified from the checkout; do not invent commands, tooling, workflows, conventions, or policies.
3. Write a missing `AGENTS.md` in the current working directory as a concise, repository-specific contributor guide. Prefer the title `# Repository Guidelines` and clear Markdown headings. Include only relevant, evidence-backed sections such as project structure, build/test/development commands, coding style and naming, testing expectations, commit and pull-request conventions, and material security or configuration notes.
4. Use concrete paths and commands where useful. Keep the guide actionable and reasonably compact, targeting roughly 200–400 words unless the repository clearly needs more detail.
5. If `.snow/config.json` does not exist, create it with exactly this content and a trailing newline:

   ```json
   {}
   ```

   Create the `.snow` directory when needed. Do not copy global settings, provider configuration, credentials, or inferred project preferences into this file.
6. Finish by reporting separately whether `AGENTS.md` and `.snow/config.json` were created or skipped because they already existed.

Do not merely propose content: inspect the repository when a guide is needed and create each missing file when it is safe and permitted to do so. Continue to respect all configured permissions and filesystem boundaries.
