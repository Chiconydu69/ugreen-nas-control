import { expect, test } from 'bun:test';
import { lint } from './workflows-policy';

const G = "github.repository == 'ugreen-nas-control/ugreen-nas-control'";
const ok = `
on: push
permissions: {}
jobs:
  a:
    if: \${{ ${G} }}
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`;

test('a guarded job with top-level permissions passes', () => {
    expect(lint('x.yml', ok)).toEqual([]);
});

test('rule 1: a job without the guard fails, checks.yml is exempt', () => {
    const bad = ok.replace(`    if: \${{ ${G} }}\n`, '');
    expect(lint('x.yml', bad).join()).toContain('job a: no repository guard');
    expect(lint('checks.yml', bad)).toEqual([]);
    expect(lint('x.yml', ok.replace(G, "github.event_name == 'push'")).length).toBe(1);
});

const target = `
on:
  pull_request_target:
permissions: {}
jobs:
  a:
    if: \${{ ${G} }}
    runs-on: ubuntu-latest
    steps:
      - env:
          BODY: \${{ github.event.issue.body }}
        run: echo "$BODY"
`;

test('rule 2: pull_request_target with env-passed text passes', () => {
    expect(lint('x.yml', target)).toEqual([]);
});

test('rule 2: checkout or inline text in pull_request_target fails; the same in a push workflow is allowed', () => {
    const checkout = target.replace('steps:\n', 'steps:\n      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1\n');
    expect(lint('x.yml', checkout).join()).toContain('actions/checkout');
    const inline = target.replace('echo "$BODY"', 'echo "${{ github.event.pull_request.title }}"');
    expect(lint('x.yml', inline).join()).toContain('inside `run:`');
    const head = target.replace('echo "$BODY"', 'git fetch ${{ github.event.pull_request.head.ref }}');
    expect(lint('x.yml', head).length).toBe(1);
    expect(lint('x.yml', checkout.replace('pull_request_target:', 'push:'))).toEqual([]);
});

test('rule 3: missing, non-empty or read-all permissions fail', () => {
    expect(lint('x.yml', ok.replace('permissions: {}\n', '')).join()).toContain('permissions');
    expect(lint('x.yml', ok.replace('permissions: {}', 'permissions: read-all')).length).toBe(1);
    expect(lint('x.yml', ok.replace('permissions: {}', 'permissions:\n  contents: read')).length).toBe(1);
});
