# A band's admin tab — members' details, GUSO, invoicing, sharing a gig's fee

Status: **notes for later** (2026-11-23), not designed or built. Written from general
knowledge of the French system; every legal point below needs checking against the
official sources (guso.fr, urssaf.fr, impots.gouv.fr) and, before building, an
accountant's read.

## What a band needs around a paid concert

1. **Who pays the musicians, and how.**
   - **GUSO** (Guichet unique du spectacle occasionnel): an organiser whose main
     business isn't live shows (a bar, a restaurant, a town hall, a private person,
     an association that doesn't run shows) and who pays artists occasionally must
     declare and pay them through GUSO (one declaration covers social contributions,
     unemployment insurance for intermittents, retirement, Congés Spectacles,
     occupational health). **The organiser declares, not the band** — the band's job
     is to hand the organiser each musician's details and the agreed amounts.
   - A **licensed producer / the band's own association** with a licence
     (licence d'entrepreneur de spectacles) employs the musicians itself and pays
     them through its own payroll; GUSO doesn't apply then.
   - **Invoicing**: when the band (an association, or a member as a company /
     micro-entrepreneur) sells the show to the organiser, it issues an invoice
     (contrat de cession); the musicians are then paid by the band's structure.
     France's e-invoicing reform (2026–2027) applies to companies — check what it
     means for associations and micro-entrepreneurs.
2. **Each member's details** an organiser asks for a GUSO declaration: legal name,
   date and place of birth, social security number (NIR), address, Congés Spectacles
   number, Audiens number, the role (musician), the dates, the number of cachets,
   the gross amount per cachet.
3. **Sharing the fee**: the gig's total, the costs (travel, sound engineer, rehearsal
   room, gear rental, the organiser's share of a door split), the split rule (equal,
   by role, fixed amounts for a dep), what each member gets, what the band's kitty
   keeps.

## What the app could do

- **Members' admin details** (per band, private): the fields above, each member
  filling their own (never the others'), shown to the band's admin role only when
  preparing a gig's paperwork; exported per gig as a sheet / CSV / PDF to send the
  organiser ("GUSO pack"), with consent recorded.
- **A gig's money page**: the fee, how it's paid (GUSO by the organiser / invoice by
  the band / cash), costs, the split (rules + overrides), who has been paid,
  documents (contract, invoice, GUSO attestation).
- **Invoices**: numbered invoices for a band structure (association / company),
  PDF, a ledger per band — or an export to the band's accounting tool rather than
  becoming one.
- **Reminders**: "send your GUSO details to <organiser> by <date>", "the GUSO
  attestation for <gig> has arrived".

## Constraints and risks

- **The NIR and identity documents are sensitive data** (RGPD; the NIR has its own
  rules): encrypt at rest, strict access (the member and the band's admin for a
  given gig), audit log, deletion on leaving, data processing documented. Consider
  *not* storing the NIR at all: generate the pack with blanks the member fills
  privately.
- The app must not look like it **makes declarations** on anyone's behalf (GUSO
  declarations are the organiser's legal act).
- Invoicing touches tax rules (VAT exemption mentions, numbering, legal mentions):
  keep it minimal or delegate to a dedicated tool.

## Next step when picked up

Interview two or three bands (Mercy, Other Lies) about how they're paid today;
check GUSO's current requirements; decide "pack + split + export" (small) vs
"invoicing" (bigger, regulated).
