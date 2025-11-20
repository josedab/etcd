# RFC Prioritization Matrix

**Analysis Commit:** `d6bc3229d81384aed0fc03a3ddd3dccfef092048`

---

## Overview

This document prioritizes all 13 proposed RFCs based on impact and effort. Use this to guide implementation order and resource allocation.

---

## Priority Categories

### P0 - Critical (Immediate Action)
Highest impact, blocks scalability or creates operational risk.

### Quick Wins (< 1 week effort)
High impact, low effort. Implement these first.

### Strategic (2-4 weeks effort)
Significant value requiring moderate investment.

### Long-term (> 1 month effort)
Architectural changes requiring careful planning.

---

## Complete RFC Summary Table

| RFC | Title | Category | Effort | Impact | User Pain | Priority |
|-----|-------|----------|--------|--------|-----------|----------|
| **P0 - Critical** |
| RFC-0009 | Tiered Key Index (Hybrid Memory) | Long-term | 10-12 weeks | Very High | High | P0 |
| RFC-0010 | Backup & Disaster Recovery | Long-term | 14-16 weeks | Very High | Very High | P0 |
| **Quick Wins** |
| RFC-0001 | Automated Vulnerability Scanning | Quick Win | 2-3 days | High | Low | P1 |
| RFC-0002 | Client Request Hedging | Quick Win | 1 week | Medium | Medium | P2 |
| **Strategic** |
| RFC-0011 | Lease Management Optimization | Strategic | 7-8 weeks | High | High | P1 |
| RFC-0003 | Server Package Decomposition | Strategic | 3-4 weeks | High | Low | P1 |
| RFC-0012 | Hot Key Detection & Mitigation | Strategic | 7-8 weeks | Medium | High | P1 |
| RFC-0013 | V2 API Complete Removal | Strategic | 4-5 weeks | High | Low | P1 |
| RFC-0004 | Adaptive Watch Batching | Strategic | 2-3 weeks | Medium | Medium | P2 |
| RFC-0005 | Structured Error Context | Strategic | 2 weeks | Medium | Medium | P3 |
| **Long-term** |
| RFC-0006 | Protobuf Library Migration | Long-term | 6-8 weeks | High | Low | P1 |
| RFC-0007 | Pluggable Storage Backend | Long-term | 8-12 weeks | High | Medium | P2 |
| RFC-0008 | Native Observability Dashboard | Long-term | 4-6 weeks | Medium | Medium | P3 |

---

## Impact vs Effort Matrix (Updated)

```
VERY HIGH IMPACT
    │
    │  ┌─────────────┐     ┌─────────────┐
    │  │ RFC-0011    │     │ RFC-0009    │
    │  │ Lease       │     │ Tiered      │
    │  │ Optimize    │     │ Index       │
    │  └─────────────┘     └─────────────┘
    │
HIGH IMPACT
    │
    │  ┌─────────────┐     ┌─────────────┐
    │  │ RFC-0001    │     │ RFC-0010    │
    │  │ Vuln Scan   │     │ Backup/DR   │
    │  │             │     │             │
    │  └─────────────┘     └─────────────┘
    │
    │  ┌─────────────┐     ┌─────────────┐
    │  │ RFC-0003    │     │ RFC-0006    │
    │  │ Server      │     │ Protobuf    │
    │  │ Decomp.     │     │ Migration   │
    │  └─────────────┘     └─────────────┘
    │
    │  ┌─────────────┐     ┌─────────────┐
    │  │ RFC-0013    │     │ RFC-0007    │
    │  │ V2 API      │     │ Pluggable   │
    │  │ Removal     │     │ Storage     │
    │  └─────────────┘     └─────────────┘
    │
MEDIUM IMPACT
    │
    │  ┌─────────────┐     ┌─────────────┐
    │  │ RFC-0002    │     │ RFC-0012    │
    │  │ Request     │     │ Hot Key     │
    │  │ Hedging     │     │ Detection   │
    │  └─────────────┘     └─────────────┘
    │
    │  ┌─────────────┐     ┌─────────────┐
    │  │ RFC-0005    │     │ RFC-0004    │
    │  │ Structured  │     │ Adaptive    │
    │  │ Errors      │     │ Watch       │
    │  └─────────────┘     └─────────────┘
    │
    │  ┌─────────────┐
    │  │ RFC-0008    │
    │  │ Dashboard   │
    │  │             │
    │  └─────────────┘
    │
LOW ├────────────────────────────────────▶
    │       LOW EFFORT         HIGH EFFORT
```

---

## Recommended Implementation Order

### Phase 0: Critical Foundations (Weeks 1-26) - START IMMEDIATELY

**RFC-0009: Tiered Key Index** (Weeks 1-12)
- **Why first:** Unblocks large-scale deployments
- **Impact:** 10M+ keys support vs current 1M limit
- **Dependencies:** None
- **Team:** 2 senior engineers

**RFC-0010: Backup & Disaster Recovery** (Weeks 1-16, parallel)
- **Why critical:** Most requested operational feature
- **Impact:** Production-ready DR without external tools
- **Dependencies:** None
- **Team:** 2-3 engineers

### Phase 1: Quick Wins (Weeks 1-2)

**RFC-0001: Automated Vulnerability Scanning**
- Immediate security benefit
- Minimal disruption
- Establishes good practices

**RFC-0002: Client Request Hedging**
- Improves client experience
- Localized change
- Easy to measure impact

### Phase 2: Strategic Improvements (Weeks 3-20)

**RFC-0011: Lease Management Optimization** (Weeks 3-10) - HIGH PRIORITY
- **Why important:** Critical for Kubernetes scale
- **Impact:** 50% CPU reduction, support 100K+ leases
- **Blocks:** Large Kubernetes clusters
- **Team:** 1-2 engineers

**RFC-0003: Server Package Decomposition** (Weeks 5-9)
- Improves maintainability
- Enables parallel development
- Reduces onboarding time

**RFC-0012: Hot Key Detection & Mitigation** (Weeks 8-16)
- **Why important:** Prevents cluster saturation
- **Impact:** Operational visibility + automatic mitigation
- **User pain:** High (ConfigMap storms, leader election)
- **Team:** 1-2 engineers

**RFC-0013: V2 API Complete Removal** (Weeks 10-15)
- **Why important:** Reduce technical debt
- **Impact:** 3,000+ lines removed, smaller binary
- **Blocks:** Long-term maintenance
- **Team:** 1-2 engineers

**RFC-0004: Adaptive Watch Batching** (Weeks 12-15)
- Performance improvement
- Addresses known pain point
- Measurable benefits

**RFC-0005: Structured Error Context** (Weeks 16-18)
- Better debugging
- Improved client experience
- Foundation for RFC-0002

### Phase 3: Long-term Architecture (Weeks 20+)

**RFC-0006: Protobuf Library Migration** (Weeks 20-28)
- Addresses deprecated dependency
- Security risk reduction
- Required for long-term maintenance

**RFC-0007: Pluggable Storage Backend** (Weeks 24-36)
- Enables optimization
- Future flexibility
- Research/prototype first

**RFC-0008: Native Observability Dashboard** (Weeks 28-34)
- Improved operations
- Lower priority, can defer

---

## Dependencies

```
Critical Path:
RFC-0009 (Tiered Index) → No dependencies, start immediately
RFC-0010 (Backup/DR) → No dependencies, start immediately
RFC-0011 (Lease Optimization) → Blocks Kubernetes scalability

Dependencies:
RFC-0005 (Structured Errors)
    └── RFC-0002 (Request Hedging) - benefits from better error context

RFC-0003 (Server Decomposition)
    └── RFC-0007 (Pluggable Storage) - cleaner interfaces help

RFC-0006 (Protobuf Migration)
    └── Should complete before major new features

RFC-0013 (V2 Removal)
    └── Clears technical debt before new work
```

---

## Resource Requirements by Phase

### Phase 0: Critical (26 weeks total, 16 weeks parallel)
- **RFC-0009:** 2 senior engineers × 12 weeks
- **RFC-0010:** 2-3 engineers × 16 weeks
- **Can run in parallel**

### Phase 1: Quick Wins
- 1 engineer, 1-2 weeks total
- Minimal review burden

### Phase 2: Strategic (20 weeks)
- **RFC-0011:** 1-2 engineers × 8 weeks (weeks 3-10)
- **RFC-0003:** 1-2 engineers × 4 weeks (weeks 5-9)
- **RFC-0012:** 1-2 engineers × 8 weeks (weeks 8-16)
- **RFC-0013:** 1-2 engineers × 5 weeks (weeks 10-15)
- **RFC-0004:** 1 engineer × 3 weeks (weeks 12-15)
- **RFC-0005:** 1 engineer × 2 weeks (weeks 16-18)

### Phase 3: Long-term
- **RFC-0006:** 1-2 engineers × 8 weeks
- **RFC-0007:** 2+ engineers × 12 weeks
- **RFC-0008:** 1-2 engineers × 6 weeks

---

## Success Criteria Summary

| RFC | Key Metric |
|-----|------------|
| RFC-0001 | 0 critical CVEs in dependencies |
| RFC-0002 | 50% reduction in P99 client latency |
| RFC-0003 | 50% reduction in server.go size |
| RFC-0004 | 30% reduction in watch-related CPU |
| RFC-0005 | 90% of errors include context |
| RFC-0006 | 0 deprecated protobuf dependencies |
| RFC-0007 | Storage backend is pluggable |
| RFC-0008 | Dashboard deployed and used |
| **RFC-0009** | **Support 10M keys in 8GB RAM** |
| **RFC-0010** | **RPO < 5min, RTO < 5min** |
| **RFC-0011** | **Support 100K leases, 50% CPU reduction** |
| **RFC-0012** | **Detect hot keys with >95% accuracy** |
| **RFC-0013** | **Remove 3,000+ LOC, 5-10% smaller binary** |

---

## Risk Assessment

### Low Risk
- RFC-0001: CI/CD change only
- RFC-0005: Additive, backward compatible
- RFC-0008: Optional component

### Medium Risk
- RFC-0002: Client behavior change
- RFC-0003: Refactoring, could break things
- RFC-0004: Performance tuning, needs testing
- RFC-0012: New subsystem, needs tuning
- RFC-0013: Breaking change (intentional)

### High Risk
- RFC-0006: Touches all generated code
- RFC-0007: Core architecture change
- RFC-0009: Major storage refactor
- RFC-0010: Complex data management
- RFC-0011: Critical path changes

---

## Stakeholder Approvals Required

| RFC | Maintainers | SIG | Community | Kubernetes |
|-----|-------------|-----|-----------|------------|
| RFC-0001 | Yes | No | No | No |
| RFC-0002 | Yes | API | No | No |
| RFC-0003 | Yes | No | No | No |
| RFC-0004 | Yes | No | No | No |
| RFC-0005 | Yes | API | No | No |
| RFC-0006 | Yes | API | Yes | No |
| RFC-0007 | Yes | Storage | Yes | No |
| RFC-0008 | Yes | No | No | No |
| **RFC-0009** | **Yes** | **Storage** | **Yes** | **No** |
| **RFC-0010** | **Yes** | **Operations** | **Yes** | **No** |
| **RFC-0011** | **Yes** | **No** | **No** | **Yes** |
| **RFC-0012** | **Yes** | **No** | **No** | **Yes** |
| **RFC-0013** | **Yes** | **API** | **Yes** | **No** |

---

## Timeline Summary (Detailed)

```
CRITICAL PATH (Start Immediately):
Week 1-12:  RFC-0009 (Tiered Index) - 2 engineers
Week 1-16:  RFC-0010 (Backup/DR) - 2-3 engineers (parallel)

QUICK WINS:
Week 1-2:   RFC-0001, RFC-0002

STRATEGIC (High Impact):
Week 3-10:  RFC-0011 (Lease Optimization) - Critical for K8s
Week 5-9:   RFC-0003 (Server Decomposition)
Week 8-16:  RFC-0012 (Hot Key Detection)
Week 10-15: RFC-0013 (V2 Removal)
Week 12-15: RFC-0004 (Watch Batching)
Week 16-18: RFC-0005 (Structured Errors)

LONG-TERM:
Week 20-28: RFC-0006 (Protobuf Migration)
Week 24-36: RFC-0007 (Pluggable Storage)
Week 28-34: RFC-0008 (Dashboard)
```

---

## Critical User Pain Points Addressed

| User Pain | RFC | Priority |
|-----------|-----|----------|
| **Cannot scale >1M keys** | RFC-0009 | P0 |
| **No automated backups** | RFC-0010 | P0 |
| **Kubernetes lease overhead** | RFC-0011 | P1 |
| **Hot ConfigMap storms** | RFC-0012 | P1 |
| **V2 maintenance burden** | RFC-0013 | P1 |
| Deprecated gogo/protobuf | RFC-0006 | P1 |
| Server.go too large | RFC-0003 | P1 |
| High tail latency | RFC-0002 | P2 |
| Watch CPU overhead | RFC-0004 | P2 |
| No hot key visibility | RFC-0012 | P1 |

---

## Next Steps

1. **Immediate:** Start RFC-0009 and RFC-0010 (critical path)
2. **Week 1:** Implement RFC-0001 (quick security win)
3. **Week 3:** Begin RFC-0011 (Kubernetes blocker)
4. **Review quarterly:** Adjust priorities based on user feedback

---

*This prioritization should be revisited quarterly as project needs evolve. Last updated: November 19, 2025*
