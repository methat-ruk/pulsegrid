# PulseGrid UI Design System

Status: Design Reference

This document defines the visual foundation for PulseGrid.
Use it as the default reference when implementing or reviewing
browser-visible UI.

The intended visual direction is:

- calm and comfortable for long operations sessions
- warm rather than pure white
- soft green / sage identity
- professional without looking overly corporate
- information-dense without feeling crowded
- restrained use of status colors
- clear hierarchy for telemetry and operational data

---

## 1. Visual Direction

PulseGrid should avoid both extremes:

- no pure white application canvas
- no near-black dashboard theme
- no highly saturated brand colors across large surfaces
- no excessive gradients or glassmorphism
- no excessive shadows

The interface should use warm neutral surfaces with muted sage and
teal-green accents.

Keywords:

`calm` · `operational` · `natural` · `technical` · `focused` · `soft`

---

## 2. Typography

### Primary Font — Manrope

Use **Manrope** as the primary UI font.

Why:

- more distinctive than Inter
- highly readable in dashboards
- works well for numbers and metrics
- modern without looking futuristic
- suitable for both headings and body text

Fallback:

font-family:
  "Manrope",
  "Noto Sans Thai",
  system-ui,
  sans-serif;

### Optional Monospace Font — JetBrains Mono

Use **JetBrains Mono** only for technical identifiers:

- device IDs
- event IDs
- trace IDs
- MQTT topics
- Kafka topics
- IP addresses
- code/configuration values

Do not use monospace for ordinary UI content.

### Type Scale

Display / Page title:
- 30px
- 700
- line-height 1.2

Section title:
- 20px
- 700

Card title:
- 16px
- 600

Body:
- 14px
- 400–500

Small / Metadata:
- 12px
- 500

Metric:
- 28–32px
- 700

---

## 3. Core Color Palette

### Application Background

Warm Sage Mist

#F3F5F0

Use for:
- main application background
- large neutral areas

Avoid #FFFFFF as the page background.

### Sidebar Background

Soft Sage

#E5ECE5

Use for:
- navigation sidebar
- secondary navigation surfaces

### Primary Surface

Warm Ivory

#FAFAF7

Use for:
- cards
- tables
- dialogs
- panels

### Secondary Surface

Muted Sage Surface

#EEF2EC

Use for:
- filters
- inactive tabs
- grouped controls
- subtle sections

### Elevated Surface

#FDFDFB

Use sparingly for:
- dropdowns
- popovers
- elevated interactive surfaces

---

## 4. Brand Colors

### Primary

Forest Teal

#276859

Use for:
- primary buttons
- selected navigation
- important interactive elements
- active states

### Primary Hover

#20574B

### Primary Soft

#D7E7DF

Use for:
- selected navigation background
- badges
- subtle highlights

### Accent Sage

#79A88F

Use for:
- charts
- decorative accents
- secondary positive visualization

Do not cover large sections of the UI with the primary brand color.

---

## 5. Text Colors

Primary text:

#17231F

Secondary text:

#56645E

Muted text:

#7A8781

Disabled text:

#A3ACA7

Inverse text:

#F7FAF8

Avoid pure black (#000000).

---

## 6. Semantic Colors

Semantic colors must communicate state, not decoration.

### Success / Online

#2E9B72

Soft:
#DDF2E8

Examples:
- device online
- command completed
- healthy system

### Warning

#D99A32

Soft:
#FAEDCF

Examples:
- degraded device
- battery warning
- rollout warning

### Critical / Error

#D95C59

Soft:
#F8DEDC

Examples:
- offline critical device
- failed command
- critical alert

### Information

#4D7FC1

Soft:
#DEE9F6

Examples:
- informational event
- configuration update

Do not use semantic red/green/yellow for ordinary branding.

---

## 7. Borders

Default:

#DDE4DE

Subtle:

#E8ECE8

Strong:

#C8D2CA

Default border width:

1px

Prefer borders and surface differences over strong shadows.

---

## 8. Shadows

Cards should feel slightly separated from the background, not floating.

Default card:

0 1px 3px rgba(24, 40, 32, 0.06)

Elevated:

0 6px 20px rgba(24, 40, 32, 0.08)

Avoid heavy shadows.

---

## 9. Border Radius

Small controls:
8px

Buttons / Inputs:
10px

Cards:
12px

Large panels:
16px

Modal:
16px

Avoid excessive pill-shaped components.

Pills are reserved for:

- status
- filters
- tags
- compact segmented controls

---

## 10. Spacing

Use a 4px base spacing system.

4
8
12
16
20
24
32
40
48

Typical usage:

Card padding:
20–24px

Page horizontal padding:
24–32px

Card gap:
16px

Section gap:
24–32px

Dense table row:
48–52px

---

## 11. Navigation

Desktop navigation uses a persistent left sidebar.

Recommended width:

240px

Structure:

PulseGrid

Overview

Operations
- Devices
- Telemetry
- Commands
- Alerts

Automation
- Rules
- Rollouts

Workspace
- Fleet Management
- Organizations

System
- Settings

Selected navigation:

background: #D7E7DF
text: #20574B

Do not make every navigation item visually prominent.

---

## 12. Cards

Cards should use:

background: #FAFAF7
border: 1px solid #DDE4DE
border-radius: 12px

Metric cards should contain:

- icon
- label
- primary value
- optional trend
- supporting context

Example:

Total Devices
342
↑ 12%
+36 from last week

Do not make every metric card a different color.

---

## 13. Tables

Tables are a primary component of PulseGrid.

Use comfortable density rather than extremely compact enterprise tables.

Table structure:

- checkbox
- primary identifier
- status
- type
- location/group
- last seen
- contextual metric
- actions

Rows should support:

- hover state
- keyboard focus
- selected state
- status indicators

Prefer subtle separators over boxed cells.

---

## 14. Status Presentation

Never communicate status using color alone.

Use:

icon + color + label

Examples:

● Online
● Offline
▲ Warning

Device health should use consistent semantic colors across:

- tables
- maps
- charts
- device detail
- alerts

---

## 15. Charts

Library:

Apache ECharts

Charts should inherit the calm visual language.

Default chart sequence:

1. #2E9B72
2. #4D7FC1
3. #79A88F
4. #D99A32
5. #8A79A8
6. #D95C59

Grid lines:

#E3E8E3

Axis labels:

#7A8781

Avoid:

- rainbow palettes
- unnecessary 3D charts
- strong gradients
- excessive animation
- decorative charts without operational meaning

Telemetry charts should prioritize readability over visual novelty.

---

## 16. Maps

Maps should use a low-contrast neutral base.

Device markers:

Online:
#2E9B72

Warning:
#D99A32

Offline:
#D95C59

Clusters may use the primary Forest Teal.

The map should not visually compete with alerts or telemetry.

---

## 17. Icons

Primary icon system:

Iconify

Recommended style:

simple outlined icons with consistent stroke weight.

Avoid mixing:

- filled icons
- outlined icons
- emoji
- unrelated icon families

within the same navigation or component group.

---

## 18. Buttons

Primary:

background: #276859
text: #F7FAF8

Primary hover:

#20574B

Secondary:

background: #EEF2EC
border: #DDE4DE
text: #263A32

Destructive actions should use red only when the action is actually
destructive.

---

## 19. Forms

Inputs should use:

background: #FAFAF7
border: #D4DDD6
radius: 10px

Focus:

border: #79A88F
ring: subtle primary soft color

Always provide visible labels.

Placeholder text must not replace labels.

---

## 20. Dashboard Hierarchy

The Overview page should answer these questions in order:

1. Is the fleet healthy?
2. Is anything requiring attention?
3. Where are the affected devices?
4. What changed recently?
5. What action can the operator take?

Recommended hierarchy:

Overview
├── Fleet summary
│   ├── Total Devices
│   ├── Online
│   ├── Offline
│   └── Active Alerts
│
├── Device Locations
├── Device Health
├── Telemetry Overview
├── Recent Alerts
├── Devices
└── Recent Activity

Infrastructure metrics such as Kafka consumer lag should not dominate
the normal operator dashboard.

They belong in operational/admin views unless directly relevant to the
user's task.

---

## 21. Responsive Behavior

Desktop:
≥ 1280px

Tablet:
768–1279px

Mobile:
< 768px

Desktop:
- persistent sidebar
- multi-column dashboard

Tablet:
- collapsible sidebar
- reduced dashboard columns

Mobile:
- navigation drawer
- single-column cards
- tables switch to prioritized columns or card representation

Do not merely horizontally shrink the desktop dashboard.

---

## 22. Accessibility

Target WCAG 2.2 AA where practical.

Required:

- keyboard navigation
- visible focus states
- sufficient text contrast
- semantic HTML
- accessible form labels
- chart alternatives / summaries where appropriate
- status must not depend on color alone
- reduced-motion support

---

## 23. Component Foundation

Use:

- Tailwind CSS v4
- Nuxt UI
- Iconify
- Apache ECharts

Nuxt UI provides component primitives.

PulseGrid owns:

- layout
- information architecture
- product workflows
- spacing decisions
- visual hierarchy
- semantic colors
- dashboard composition

Do not treat the default Nuxt UI appearance as the PulseGrid design
system.

---

## 24. Design Tokens

Implementation should eventually expose these values as semantic
tokens rather than scattering hex values throughout components.

Example:

--color-bg-app: #F3F5F0;
--color-bg-sidebar: #E5ECE5;

--color-surface: #FAFAF7;
--color-surface-secondary: #EEF2EC;

--color-primary: #276859;
--color-primary-hover: #20574B;
--color-primary-soft: #D7E7DF;

--color-text-primary: #17231F;
--color-text-secondary: #56645E;
--color-text-muted: #7A8781;

--color-border: #DDE4DE;

--color-success: #2E9B72;
--color-warning: #D99A32;
--color-danger: #D95C59;
--color-info: #4D7FC1;

---

## 25. Design Review Checklist

Before considering a browser-visible change complete, verify:

- follows PulseGrid design tokens
- no arbitrary colors where a semantic token exists
- typography follows the defined hierarchy
- loading state exists where applicable
- empty state exists where applicable
- error state exists where applicable
- success state is clear
- disabled state is clear
- keyboard focus is visible
- status does not rely only on color
- responsive behavior has been checked
- information density remains readable
- browser verification has been performed

---

## Design Principle

> Calm surfaces. Clear status. Actionable information.

PulseGrid should feel like an operations tool that can remain open all
day without becoming visually tiring.
