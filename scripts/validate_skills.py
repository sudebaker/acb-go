#!/usr/bin/env python3
"""
Validador de Skills para ACB (Agent Communication Bus)
Este script valida que las skills cumplan con las reglas definidas en docs/SKILL_NAMING.md

Uso:
    python3 validate_skills.py [--strict] skill1 skill2 ...
    python3 validate_skills.py [--strict] < file_with_skills.txt

Ejemplos:
    python3 validate_skills.py python docker-testing go
    python3 validate_skills.py --strict < skills.txt
"""

import argparse
import re
import sys


RESERVED_PREFIXES = ["system:", "internal:", "admin:", "acb:"]

MAX_LENGTH = 64
MIN_LENGTH = 2
MAX_WORDS = 4


def validate_skill(skill: str, strict: bool = False) -> tuple[bool, str | None]:
    """
    Valida una single skill against all constraints.
    
    Returns:
        tuple: (is_valid, error_message or None)
    """
    # Empty check
    if not skill or skill.strip() == "":
        return False, "empty or whitespace-only"
    
    # Length constraints
    if len(skill) < MIN_LENGTH:
        return False, f"too short (min {MIN_LENGTH} chars)"
    
    if len(skill) > MAX_LENGTH:
        return False, f"too long (max {MAX_LENGTH} chars)"
    
    # Must be lowercase
    if skill != skill.lower():
        return False, "must be lowercase"
    
    # No spaces
    if any(c in skill for c in " \t\n\r"):
        return False, "contains spaces"
    
    # Max words check
    words = skill.split("-")
    if len(words) > MAX_WORDS:
        return False, f"too many words (max {MAX_WORDS})"
    
    # Only alphanumeric + hyphen allowed
    pattern = r'^[a-z0-9-]+$'
    if not re.match(pattern, skill):
        return False, "invalid characters (only lowercase letters, numbers, and hyphens allowed)"
    
    # Reserved prefixes
    for prefix in RESERVED_PREFIXES:
        if skill.startswith(prefix):
            return False, f"reserved prefix '{prefix}'"
    
    return True, None


def validate_skills(skills: list[str], strict: bool = False) -> tuple[bool, list[str]]:
    """
    Valida múltiples skills y detecta duplicados.
    
    Args:
        skills: List of skill strings to validate
        strict: If True, fail fast on first error
    
    Returns:
        tuple: (all_valid, list_of_errors)
    """
    errors = []
    
    # Check for duplicates
    seen = set()
    for skill in skills:
        if skill in seen:
            errors.append(f"duplicate skill: '{skill}'")
            if strict:
                return False, errors
        seen.add(skill)
    
    # Validate each skill
    for skill in skills:
        is_valid, error = validate_skill(skill, strict=strict)
        if not is_valid:
            errors.append(f"'{skill}': {error}")
            if strict:
                return False, errors
    
    return len(errors) == 0, errors


def main():
    parser = argparse.ArgumentParser(
        description="Validador de Skills para ACB",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Ejemplos:
    python3 validate_skills.py python docker-testing go
    python3 validate_skills.py --strict < skills.txt
    python3 validate_skills.py Python cosas de marte system:core
        """
    )
    parser.add_argument(
        "skills",
        nargs="*",
        help="List of skills to validate (or read from stdin if not provided)"
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="Fail fast on first error"
    )
    
    args = parser.parse_args()
    
    # Read skills from stdin if not provided via args
    if not args.skills:
        skills = []
        for line in sys.stdin:
            skill = line.strip()
            if skill:
                skills.append(skill)
    else:
        skills = args.skills
    
    if not skills:
        print("Error: No skills provided", file=sys.stderr)
        sys.exit(1)
    
    is_valid, errors = validate_skills(skills, strict=args.strict)
    
    if is_valid:
        print(f"✅ All {len(skills)} skills are valid!")
        sys.exit(0)
    else:
        print(f"❌ Found {len(errors)} error(s):", file=sys.stderr)
        for error in errors:
            print(f"   - {error}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
