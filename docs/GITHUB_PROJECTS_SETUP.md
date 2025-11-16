# GitHub Projects Setup Guide

## Recommended Structure

### Project Board: "GraphDB Roadmap"

Use GitHub Projects (new experience) with these views:

#### 1. **Roadmap View** (Timeline)
Shows milestones and major features on a timeline

#### 2. **Board View** (Kanban)
Columns: Backlog → Planning → In Progress → Review → Done

#### 3. **Table View**
All issues with custom fields for filtering

## Suggested Epics (as Issues with "epic" label)

### Epic 1: Core Durability ✅ COMPLETE
**Status**: Done (Milestone 2)
**Issues**:
- [x] TDD Iteration 2: Double-close protection
- [x] TDD Iteration 3: Disk-backed edge durability
- [x] TDD Iteration 4: Edge deletion durability
- [x] TDD Iteration 5: Node deletion durability (2 bugs)
- [x] TDD Iteration 6: Property index WAL durability
- [x] TDD Iteration 7: Batched WAL validation
- [x] TDD Iteration 8: Snapshot durability
- [x] TDD Iteration 10: UpdateNode WAL durability

**Outcome**: 9 critical bugs found and fixed

### Epic 2: Concurrency & Performance ✅ COMPLETE
**Status**: Done (Milestone 2)
**Issues**:
- [x] TDD Iteration 9: Concurrent operations (race condition fixed)
- [x] TDD Iteration 11: Label/type index durability
- [x] Disk-backed adjacency lists with LRU cache
- [x] Batched WAL for performance

**Outcome**: Thread-safe operations, 5x memory improvement

### Epic 3: Production Readiness 🔄 IN PROGRESS
**Status**: Partially complete
**Issues**:
- [x] Automated upgrade system
- [x] Replication (primary-replica)
- [x] Capacity testing (5M+ nodes)
- [ ] Distributed queries
- [ ] Cluster coordination
- [ ] Production monitoring
- [ ] Backup/restore tools

### Epic 4: Query Optimization 📋 PLANNED
**Status**: Planned (Milestone 3)
**Issues**:
- [ ] Query planner optimization
- [ ] Index selection algorithm
- [ ] Query result caching
- [ ] Parallel query execution
- [ ] Query performance profiling

### Epic 5: Advanced Features 📋 PLANNED
**Status**: Future
**Issues**:
- [ ] Graph algorithms library expansion
- [ ] Full-text search integration
- [ ] Time-travel queries (historical snapshots)
- [ ] Graph visualization API
- [ ] Schema validation

## Custom Fields

### Field: Epic
- Type: Single select
- Options: Core Durability, Concurrency, Production, Queries, Features

### Field: Priority
- Type: Single select
- Options: Critical, High, Medium, Low

### Field: Effort
- Type: Number
- Description: Story points or hours

### Field: Status
- Type: Status (built-in)
- Options: Backlog, Planning, In Progress, Review, Done

### Field: Milestone
- Type: Milestone (built-in)
- Link to GitHub milestones

## Labels to Create

```
epic          # Purple - For epic tracking issues
bug:critical  # Red - Critical bugs (like TDD found)
bug:major     # Orange - Major bugs
enhancement   # Blue - Feature enhancements
performance   # Yellow - Performance improvements
durability    # Green - Durability/reliability
concurrency   # Teal - Threading/concurrency
tdd           # Pink - TDD-driven work
milestone-1   # Gray - Milestone 1 work
milestone-2   # Gray - Milestone 2 work
milestone-3   # Gray - Milestone 3 work
```

## Creating the Project

1. Go to repository → Projects → New Project
2. Choose "Team Planning" template or "Board" template
3. Add custom fields above
4. Create epic issues with task lists
5. Link existing commits/PRs to issues

## Example Epic Issue Template

```markdown
# Epic: Production Readiness

## Overview
Make GraphDB production-ready with monitoring, backup, and operational tools.

## Goals
- [ ] Production monitoring and metrics
- [ ] Backup and restore capabilities
- [ ] Cluster management tools
- [ ] Deployment automation
- [ ] Production runbooks

## Success Criteria
- [ ] Can deploy to production with monitoring
- [ ] Can backup/restore entire database
- [ ] Can manage multi-node cluster
- [ ] Documentation for operators

## Related Issues
- #XX - Production monitoring
- #XX - Backup system
- #XX - Cluster coordination

## Dependencies
- Depends on Milestone 2 completion ✅
- Depends on replication system ✅

## Timeline
- Start: After Milestone 2 ✅
- Target: Q1 2025
```

## Integration with Existing Workflow

### Keep Current Practices
- ✅ Milestone-based development (docs/milestones/)
- ✅ TDD iteration reports (docs/tdd/)
- ✅ Comprehensive documentation
- ✅ Feature branches with good commit messages

### Add Project Management
- Create issues for planned work
- Link commits to issues with "#123" in commit message
- Update project board as work progresses
- Use project for visibility, docs for detail

## Benefits for This Project

1. **Visibility**: Stakeholders can see roadmap at a glance
2. **Planning**: Easily see what's next after Milestone 2
3. **Tracking**: Link TDD iterations to broader epics
4. **Coordination**: If you add contributors, clear task ownership
5. **Retrospectives**: Easy to see velocity and completed work

## When to Use Issues vs. TDD Reports

| Use GitHub Issues | Use TDD Reports (docs/tdd/) |
|-------------------|----------------------------|
| Planning future work | Documenting bugs found |
| Tracking in-progress tasks | Recording fixes implemented |
| Assigning work to people | Detailed technical analysis |
| Linking commits/PRs | Lessons learned |
| Public roadmap | Historical record |

**Both complement each other!**

## Getting Started

### Quick Start (30 minutes)
1. Create GitHub Project: "GraphDB Roadmap"
2. Add 5 epic issues (copy templates above)
3. Create labels
4. Add custom fields
5. Import 3-5 planned features as issues
6. Done!

### Full Setup (2 hours)
1. Create all epic issues with full task lists
2. Link historical commits to epics (if desired)
3. Set up automation rules (auto-move cards)
4. Create milestone targets
5. Document process for contributors

## Maintenance

- **Weekly**: Update issue statuses
- **After each TDD iteration**: Link to epic, mark tasks done
- **Monthly**: Review roadmap, adjust priorities
- **Per milestone**: Create new epic for next work

## Alternative: Minimal Approach

If full projects feel like overkill:

1. **Just use Milestones + Labels**
   - Create Milestone 3, 4, etc.
   - Use "epic:production" style labels
   - Create issues for planned work
   - Keep it lightweight

2. **Or keep current approach**
   - Your documentation is excellent
   - Add issues only when needed
   - Use projects later if complexity grows

## My Recommendation

Given your project's maturity:

**Start with Projects + Epic issues**
- It's not much overhead
- Complements your excellent docs
- Useful when others discover your project
- Easy to maintain with your TDD workflow
- Makes roadmap public and discoverable

You've built something impressive (9 critical bugs caught via TDD!). A GitHub Project would help showcase your roadmap and make it easy for potential contributors or users to see what's coming.
