<?php

declare(strict_types=1);

// PHP formatter (PER-CS 2.0). Unraid .page files are not formatted: a key=value header and `---` precede the PHP,
// which php-cs-fixer cannot parse. `just php-lint` syntax-checks their PHP part.
$finder = PhpCsFixer\Finder::create()
    ->in([__DIR__ . '/plugin', __DIR__ . '/tools/harness'])
    ->name('*.php');

return (new PhpCsFixer\Config())
    ->setRiskyAllowed(false)
    ->setRules(['@PER-CS2x0' => true])
    ->setIndent('    ')
    ->setLineEnding("\n")
    ->setFinder($finder);
