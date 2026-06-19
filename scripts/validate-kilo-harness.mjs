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

  const skillPaths = config.skills?.paths;
  if (!Array.isArray(skillPaths)) {
    fail(".kilo/kilo.jsonc: skills.paths must be an array");
    return;
  }

  if (!skillPaths.includes(".kilo/skills")) {
    fail('.kilo/kilo.jsonc: skills.paths must include ".kilo/skills"');
  }
}

function validateSkills() {
  const skillsDir = relPath(".kilo", "skills");
  if (!fs.existsSync(skillsDir)) {
    fail(".kilo/skills: missing directory");
    return;
  }

  const names = new Map();
  const entries = fs
    .readdirSync(skillsDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .sort((a, b) => a.name.localeCompare(b.name));

  for (const entry of entries) {
    const relativePath = path.join(".kilo", "skills", entry.name, "SKILL.md");
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

function validateMarkers(relativePath, markers) {
  const text = readText(relativePath);
  for (const marker of markers) {
    if (!text.includes(marker)) {
      fail(`${relativePath}: missing marker ${marker}`);
    }
  }
}

validateConfig();
validateSkills();
validateMarkers(".kilo/learnings/LEARNINGS.md", [
  "<!-- LEARNINGS:START -->",
  "<!-- LEARNINGS:END -->",
]);
validateMarkers(".kilo/learnings/ERRORS.md", [
  "<!-- ERRORS:START -->",
  "<!-- ERRORS:END -->",
]);
validateMarkers(".kilo/learnings/HARNESS_CHANGELOG.md", [
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
