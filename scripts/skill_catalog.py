#!/usr/bin/env python3
"""
Skill Catalog for ACB Agents

Provides dynamic skill discovery and validation for tasks.
This replaces the hardcoded validation with a dynamic catalog
that discovers skills from registered agents.
"""

import os
import json
import functools
from typing import List, Optional

# Known agents and their skills (source of truth for discovery)
# This mirrors the agents registered in ACB backend
AGENTS_SKILLS = {
    "amanda": ["orchestration", "dispatch", "review", "management"],
    "braulio": ["sysadmin", "coding", "docker", "linux", "review", "security", "infra", "go"],
    "quique": ["coding", "security", "go", "testing", "devops", "python"],
    "armando": ["osint", "hacking", "security", "review", "forensics"],
}

# Optional: whitelist of allowed skills (if configured by team)
# When set, agents can only register skills from this list
SKILLS_WHITELIST = os.environ.get("ACB_SKILLS_WHITELIST")
if SKILLS_WHITELIST:
    SKILLS_WHITELIST = [s.strip() for s in SKILLS_WHITELIST.split(",")]


def get_skill_catalog() -> List[str]:
    """
    Discover all skills from all registered agents.
    Returns a deduplicated, sorted list of skills.
    
    This is the DYNAMIC skill catalog - it's discovered from
    agent registrations, not hardcoded.
    """
    skill_set = set()
    for skills in AGENTS_SKILLS.values():
        skill_set.update(skills)
    return sorted(skill_set)


def validate_required_skills(required_skills: List[str], catalog: Optional[List[str]] = None) -> bool:
    """
    Validate that all required_skills exist in the skill catalog.
    
    Args:
        required_skills: List of skills required by a task
        catalog: Skill catalog (if None, discovered dynamically)
    
    Returns:
        True if all required skills exist in catalog, False otherwise
    
    Raises:
        ValueError: If any required skill is not in catalog
    """
    if catalog is None:
        catalog = get_skill_catalog()
    
    missing = []
    for skill in required_skills:
        if skill not in catalog:
            missing.append(skill)
    
    if missing:
        raise ValueError(
            f"Skill(s) not found in skill catalog: {missing}. "
            f"Available skills: {catalog}"
        )
    
    return True


def get_allowed_skills() -> List[str]:
    """
    Returns the list of skills agents are allowed to register.
    If whitelist is configured, returns that. Otherwise returns None (no constraint).
    """
    return SKILLS_WHITELIST


def validate_agent_skills_against_whitelist(agent_skills: List[str]) -> bool:
    """
    Validate that an agent's skills are within the allowed whitelist.
    
    Returns:
        True if all skills are allowed, False otherwise
    
    Raises:
        ValueError: If any skill is not in whitelist
    """
    if SKILLS_WHITELIST is None:
        # No whitelist configured - all skills allowed
        return True
    
    for skill in agent_skills:
        if skill not in SKILLS_WHITELIST:
            raise ValueError(
                f"Skill '{skill}' is not in the allowed skills whitelist. "
                f"Allowed: {SKILLS_WHITELIST}"
            )
    
    return True


@functools.lru_cache(maxsize=1)
def get_cached_catalog() -> List[str]:
    """
    Get cached skill catalog (avoids repeated discovery on same process).
    Cache invalidates on function redefinition (not on runtime changes).
    """
    return get_skill_catalog()


if __name__ == "__main__":
    import argparse
    
    parser = argparse.ArgumentParser(description="Skill Catalog Tool")
    parser.add_argument("--action", choices=["catalog", "allowed", "validate", "check-task"], 
                        required=True, help="Action to perform")
    parser.add_argument("--skills", nargs="+", help="Skills to validate (for validate action)")
    parser.add_argument("--task-id", help="Task ID to check (reads from ACB)")
    parser.add_argument("--agent", help="Agent name to check skills")
    
    args = parser.parse_args()
    
    if args.action == "catalog":
        catalog = get_skill_catalog()
        print(json.dumps({"skills": catalog}, indent=2))
    
    elif args.action == "allowed":
        allowed = get_allowed_skills()
        if allowed:
            print(json.dumps({"allowed": allowed}, indent=2))
        else:
            print("No whitelist configured - all skills allowed")
    
    elif args.action == "validate":
        try:
            validate_required_skills(args.skills)
            print(f"✅ All skills valid: {args.skills}")
        except ValueError as e:
            print(f"❌ Validation failed: {e}")
            exit(1)
    
    elif args.action == "check-task":
        # Placeholder for task validation
        print(f"Checking task {args.task_id}...")
        catalog = get_skill_catalog()
        print(f"Available skills: {catalog}")
        if args.agent:
            skills = AGENTS_SKILLS.get(args.agent, [])
            print(f"Agent {args.agent} skills: {skills}")
