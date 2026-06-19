#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";

const root = process.cwd();
const errors = [];

function fail(message) {
  errors.push(message);
}

function relPath(...parts) {
  return path.join(root, ...parts);
}

function readText(relativePath) {
  const absolutePath = relPath(relativePath);
  if (!fs.existsSync(absolutePath)) {
    fail(`${relativePath}: missing`);
    return "";
  }
  return fs.readFileSync(absolutePath, "utf8");
}

function stripJsonComments(input) {
  let output = "";
  let inString = false;
  let quote = "";
  let escaped = false;

  for (let i = 0; i < input.length; i += 1) {
    const char = input[i];
    const next = input[i + 1];

    if (inString) {
      output += char;
      if (escaped) {
        escaped = false;
      } else if (char === "\\") {
        escaped = true;
      } else if (char === quote) {
        inString = false;
        quote = "";
      }
      continue;
    }

    if (char === "\"" || char === "'") {
      inString = true;
      quote = char;
      output += char;
      continue;
    }

    if (char === "/" && next === "/") {
      while (i < input.length && input[i] !== "\n") i += 1;
      output += "\n";
      continue;
    }

    if (char === "/" && next === "*") {
      i += 2;
      while (i < input.length && !(input[i] === "*" && input[i + 1] === "/")) {
        if (input[i] === "\n") output += "\n";
        i += 1;
      }
      i += 1;
      continue;
    }

    output += char;
  }

  return output;
}

function parseJsonc(relativePath) {
  const text = readText(relativePath);
  if (!text) return undefined;

  try {
    return JSON.parse(stripJsonComments(text));
  } catch (error) {
    fail(`${relativePath}: invalid JSONC: ${error.message}`);
    return undefined;
  }
}

function parseFrontmatter(relativePath) {
  const text = readText(relativePath);
  if (!text.startsWith("---\n")) {
    fail(`${relativePath}: missing opening frontmatter fence`);
    return { frontmatter: {}, lines: [] };
  }

  const end = text.indexOf("\n---", 4);
  if (end === -1) {
    fail(`${relativePath}: missing closing frontmatter fence`);
    return { frontmatter: {}, lines: [] };
  }

  const lines = text.slice(4, end).split("\n");
  const frontmatter = {};
  for (const line of lines) {
    const match = line.match(/^([A-Za-z0-9_-]+):\s*(.*)$/);
    if (!match) continue;
    frontmatter[match[1]] = match[2].trim().replace(/^["']|["']$/g, "");
  }

  return { frontmatter, lines };
}

function validateConfig() {
  const config = parseJsonc(".kilo/kilo.jsonc");
  if (!config) return;

  const instructions = config.instructions;
  if (!Array.isArray(instructions)) {
    fail(".kilo/kilo.jsonc: instructions must be an array");
  } else if (!instructions.includes(".agents/rules/always.md")) {
    fail('.kilo/kilo.jsonc: instructions must include ".agents/rules/always.md"');
  }

  const skillPaths = config.skills?.paths;
  if (!Array.isArray(skillPaths)) {
    fail(".kilo/kilo.jsonc: skills.paths must be an array");
    return;
  }

  if (new Set(skillPaths).size !== skillPaths.length) {
    fail(".kilo/kilo.jsonc: skills.paths must not contain duplicates");
  }

  if (!skillPaths.includes(".agents/skills")) {
    fail('.kilo/kilo.jsonc: skills.paths must include ".agents/skills"');
  }
}

function validateContains(relativePath, requiredSnippets) {
  const text = readText(relativePath);
  for (const snippet of requiredSnippets) {
    if (!text.includes(snippet)) {
      fail(`${relativePath}: missing required reference ${snippet}`);
    }
  }
}

function validateSkillDirectory(skillPath) {
  const skillsDir = relPath(skillPath);
  if (!fs.existsSync(skillsDir)) {
    fail(`${skillPath}: missing directory`);
    return;
  }

  const names = new Map();
  const entries = fs
    .readdirSync(skillsDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .sort((a, b) => a.name.localeCompare(b.name));

  for (const entry of entries) {
    const relativePath = path.join(skillPath, entry.name, "SKILL.md");
    const absolutePath = relPath(relativePath);
    if (!fs.existsSync(absolutePath)) {
      fail(`${relativePath}: missing`);
      continue;
    }

    const { frontmatter, lines } = parseFrontmatter(relativePath);
    const name = frontmatter.name;
    const description = frontmatter.description;

    if (!name) {
      fail(`${relativePath}: frontmatter.name is required`);
    } else {
      if (!/^[a-z0-9-]{1,64}$/.test(name)) {
        fail(`${relativePath}: name must match /^[a-z0-9-]{1,64}$/`);
      }
      if (name !== entry.name) {
        fail(`${relativePath}: name "${name}" must match directory "${entry.name}"`);
      }
      if (names.has(name)) {
        fail(`${relativePath}: duplicate skill name also used by ${names.get(name)}`);
      } else {
        names.set(name, relativePath);
      }
    }

    const descriptionIndex = lines.findIndex((line) => line.startsWith("description:"));
    const descriptionLine = descriptionIndex === -1 ? undefined : lines[descriptionIndex];
    if (!descriptionLine) {
      fail(`${relativePath}: frontmatter.description is required`);
    } else if (/^description:\s*[>|]/.test(descriptionLine)) {
      fail(`${relativePath}: description must be a single line, not YAML block style`);
    } else if (!description) {
      fail(`${relativePath}: description must not be empty`);
    } else if (description.length > 1024) {
      fail(`${relativePath}: description exceeds 1024 chars`);
    }

    for (const line of lines.slice(descriptionIndex + 1)) {
      if (line.trim() === "") continue;
      if (/^[A-Za-z0-9_-]+:\s*/.test(line)) break;
      if (/^[ \t]/.test(line)) {
        fail(`${relativePath}: description must not use indented continuation lines`);
      }
      break;
    }
  }
}

function validateSkills() {
  validateSkillDirectory(".agents/skills");

  if (fs.existsSync(relPath(".kilo", "skills"))) {
    validateSkillDirectory(".kilo/skills");
  }
}

function validateMarkers(relativePath, markers) {
  const text = readText(relativePath);
  for (const marker of markers) {
    if (!text.includes(marker)) {
      fail(`${relativePath}: missing marker ${marker}`);
    }
  }
}

function validateMarkersIfExists(relativePath, markers) {
  if (fs.existsSync(relPath(relativePath))) {
    validateMarkers(relativePath, markers);
  }
}

function validateSharedLayer() {
  validateContains("AGENTS.md", [".agents/rules/always.md"]);
  validateContains(".agents/rules/always.md", [
    ".agents/rules/search.md",
    ".agents/rules/goal-loop.md",
    ".agents/rules/harness-evolution.md",
  ]);
  validateContains(".kilo/kilo.jsonc", [".agents/rules/always.md", ".agents/skills"]);
  validateContains(".kilo/rules/ast-index.md", [".agents/rules/search.md"]);
  validateContains(".kilo/rules/keep_going_until_you_reach_the_goal.md", [
    ".agents/rules/goal-loop.md",
  ]);
  validateContains(".agents/docs/evolve.md", [".agents/skills", ".agents/learnings"]);
  validateContains(".agents/skills/evolve/SKILL.md", [".agents/docs/evolve.md"]);
  validateContains(".agents/skills/consensus-orchestrator/SKILL.md", [
    ".agents/docs/consensus-orchestrator.md",
  ]);
  validateContains(".kilo/command/evolve.md", [".agents/docs/evolve.md"]);
  validateContains(".kilo/command/consensus.md", [".agents/docs/consensus-orchestrator.md"]);
  validateContains(".kilo/agent/goal-runner.md", [".agents/docs/goal-runner.md"]);
  validateContains(".kilo/agent/consensus-security.md", [".agents/docs/consensus-roles/security.md"]);
  validateContains(".kilo/agent/consensus-go-senior.md", [".agents/docs/consensus-roles/go-senior.md"]);
  validateContains(".kilo/agent/consensus-product-systems-designer.md", [
    ".agents/docs/consensus-roles/product-systems-designer.md",
  ]);
  validateContains(".kilo/agent/consensus-cli-architect.md", [
    ".agents/docs/consensus-roles/cli-architect.md",
  ]);
  validateContains(".kilo/agent/consensus-ai-first.md", [".agents/docs/consensus-roles/ai-first.md"]);
  validateContains(".kilo/agent/consensus-judge.md", [".agents/docs/consensus-roles/judge.md"]);
  validateContains(".kilo/skills/evolve/SKILL.md", [".agents/docs/evolve.md"]);
  validateContains(".kilo/skill/consensus-orchestrator/SKILL.md", [
    ".agents/docs/consensus-orchestrator.md",
  ]);
}

validateConfig();
validateSkills();
validateSharedLayer();
validateMarkers(".agents/learnings/LEARNINGS.md", [
  "<!-- LEARNINGS:START -->",
  "<!-- LEARNINGS:END -->",
]);
validateMarkers(".agents/learnings/ERRORS.md", [
  "<!-- ERRORS:START -->",
  "<!-- ERRORS:END -->",
]);
validateMarkers(".agents/learnings/HARNESS_CHANGELOG.md", [
  "<!-- HARNESS-CHANGELOG:START -->",
  "<!-- HARNESS-CHANGELOG:END -->",
]);
validateMarkersIfExists(".kilo/learnings/LEARNINGS.md", [
  "<!-- LEARNINGS:START -->",
  "<!-- LEARNINGS:END -->",
]);
validateMarkersIfExists(".kilo/learnings/ERRORS.md", [
  "<!-- ERRORS:START -->",
  "<!-- ERRORS:END -->",
]);
validateMarkersIfExists(".kilo/learnings/HARNESS_CHANGELOG.md", [
  "<!-- HARNESS-CHANGELOG:START -->",
  "<!-- HARNESS-CHANGELOG:END -->",
]);

if (errors.length > 0) {
  console.error("Harness validation failed:");
  for (const error of errors) {
    console.error(`- ${error}`);
  }
  process.exit(1);
}

console.log("Harness validation OK");
