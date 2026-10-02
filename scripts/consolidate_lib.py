# -*- coding: utf-8 -*-
"""Alias unification for domain consolidation, applied per source file.

Rewriting each source's qualified references to a canonical alias BEFORE the
bodies are concatenated is what makes the merge safe: inside one source file an
alias unambiguously denotes one import path, so `event.Foo` there can be resolved
without guessing. Doing it after concatenation cannot distinguish two packages
that share a base name.
"""
import io, os, re, subprocess


def read_source(path, base='HEAD'):
    """Read a source file's pristine text.

    Baseline first: a merge target is often also a source, so its worktree copy is
    already-merged output. Reading the worktree there would concatenate merged text
    with original text and duplicate every declaration. The baseline revision is the
    only stable source of truth; the worktree is consulted for files it does not hold.
    """
    r = subprocess.run(['git', 'show', base + ':' + path], capture_output=True, text=True)
    if r.returncode == 0:
        return r.stdout
    if os.path.exists(path):
        return io.open(path, encoding='utf-8').read()
    raise SystemExit('cannot read %s (not in %s, not on disk)' % (path, base))


def base_name(path):
    return path.rsplit('/', 1)[-1]


def split_imports(text):
    """Return (package, {path: alias}, body_without_package_and_imports)."""
    lines = text.split('\n')
    pi = next(i for i, l in enumerate(lines) if re.match(r'^package\s+\w+\s*$', l))
    pkg = re.match(r'^package\s+(\w+)\s*$', lines[pi]).group(1)
    imports, body = {}, list(lines[:pi])
    i, n, seen = pi + 1, len(lines), False
    # Block comments are tracked because a file may be disabled by wrapping its body
    # in one /* ... */: a line that reads `import (` inside such a comment is not an
    # import declaration, and hoisting it would fabricate live unused imports while the
    # code that used them stays commented out (this is how rl/http_api_test.go is
    # committed today).
    in_block = False
    for l in lines[:pi]:
        if re.match(r'^\s*/\*', l):
            in_block = True
        if in_block and '*/' in l:
            in_block = False
    while i < n:
        l, ls = lines[i], lines[i].strip()
        if in_block:
            body.append(l)
            if '*/' in l:
                in_block = False
            i += 1
            continue
        if re.match(r'^\s*/\*', l):
            in_block = '*/' not in l
            body.append(l)
            i += 1
            continue
        if not seen:
            if ls.startswith('import ('):
                j = i + 1
                while j < n and lines[j].strip() != ')':
                    inner = lines[j].strip()
                    if inner and not inner.startswith('//'):
                        m = re.match(r'^(?:([.\w]+)\s+)?"([^"]+)"$', inner)
                        if m:
                            imports[m.group(2)] = m.group(1) or ''
                    j += 1
                i = j + 1
                continue
            m = re.match(r'^import\s+(?:([.\w]+)\s+)?"([^"]+)"$', ls)
            if m:
                imports[m.group(2)] = m.group(1) or ''
                i += 1
                continue
            if ls == '' or ls.startswith('//'):
                body.append(l)
                i += 1
                continue
            if re.match(r'^(func|var|const|type)\b', ls):
                seen = True
        body.append(l)
        i += 1
    while body and body[0].strip() == '':
        body.pop(0)
    return pkg, imports, '\n'.join(body).strip('\n')


def canonical_aliases(per_source, module_prefix):
    """Pick one alias per import path across the group.

    Majority alias wins; a base-name clash between two different paths forces
    distinct aliases, synthesised from repo convention (trpc* for the upstream
    module, tagent* for this module).
    """
    users = {}
    for imports in per_source:
        for path, alias in imports.items():
            users.setdefault(path, []).append(alias)

    chosen = {}
    for path, aliases in users.items():
        nonempty = [a for a in aliases if a]
        base = base_name(path)
        chosen[path] = max(set(nonempty), key=nonempty.count) if nonempty else ''
        if not chosen[path] and _base_clashes(path, users, base):
            chosen[path] = _synthesize(path, base, module_prefix)

    # A base name shared by two paths must not end up with the same spelling.
    seen = {}
    for path in sorted(chosen):
        eff = chosen[path] or base_name(path)
        if eff in seen:
            chosen[path] = _synthesize(path, base_name(path), module_prefix)
            while chosen[path] in seen:
                chosen[path] += 'x'
        seen[chosen[path]] = path
    return chosen


def _base_clashes(path, users, base):
    return sum(1 for other in users if base_name(other) == base and other != path) > 0


def _synthesize(path, base, module_prefix):
    if path.startswith(module_prefix):
        return 'tagent' + base[0].upper() + base[1:]
    if path.startswith('trpc.group/'):
        return 'trpc' + base[0].upper() + base[1:]
    return base + 'pkg'


def unify(pkg, imports, body, chosen, module_prefix):
    """Rewrite this source's qualifiers to the canonical aliases."""
    for path, alias in imports.items():
        want = chosen.get(path, alias)
        have = alias or base_name(path)
        if want == '':
            want = base_name(path)
        if have != want:
            body = re.sub(r'\b' + re.escape(have) + r'\.', want + '.', body)
    return body


def render(pkg, chosen, imports_union, bodies):
    std = sorted(p for p in imports_union if '.' not in base_name(p) or '/' not in p)
    std = sorted(p for p in imports_union if not _third_party(p))
    third = sorted(p for p in imports_union if _third_party(p))
    head = ['package ' + pkg, '', 'import (']
    for p in std:
        head.append('\t"%s"' % p)
    if third:
        head.append('')
        for p in third:
            alias = chosen.get(p) or ''
            if alias and alias != base_name(p):
                head.append('\t%s "%s"' % (alias, p))
            elif alias:
                head.append('\t%s "%s"' % (alias, p))
            else:
                head.append('\t"%s"' % p)
    head.append(')')
    return '\n'.join(head) + '\n\n' + '\n\n'.join(b for b in bodies if b.strip()) + '\n'


def _third_party(path):
    return '.' in path.split('/')[0]
