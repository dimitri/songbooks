package store

import (
	"context"
	"fmt"
)

// Notification policy for proposals (JUDGMENT CALL): notifying every
// band member on every proposal CREATION would be noisy — a band that
// uses the listening-party flow could easily generate a dozen proposals
// in one sitting. So creation deliberately sends no notification at all
// (see CreateProposal's doc comment); discovery happens through the
// "changes since last visit" digest instead.
//
// The one notification this phase does send is on DECISION (accept or
// reject), to the proposer specifically — same one-recipient pattern as
// the band-invite-accepted notification from the previous phase — since
// "did my proposal get picked up" is the one thing a proposer can't
// discover any other way without repeatedly checking back. This fires
// even when the proposer is the one who decided it (manual self-accept,
// or their own vote happened to cross the threshold) rather than special
// -casing that away, to keep the rule simple: "a decision always leaves
// a notification in the proposer's inbox."
func (st *Store) notifyProposalDecided(ctx context.Context, p *BandProposal, newStatus string) error {
	band, err := st.GetBand(ctx, p.BandID)
	if err != nil {
		return err
	}
	var what string
	switch p.Kind {
	case "new_song":
		title := p.TargetTitle
		if title == "" {
			title = fmt.Sprintf("variant %d", *p.TargetVariantID)
		}
		what = fmt.Sprintf("your proposal to add %q to %s's repertoire", title, band.Name)
	case "new_reference":
		title := p.TargetTitle
		if title == "" {
			title = fmt.Sprintf("variant %d", *p.TargetVariantID)
		}
		what = fmt.Sprintf("your proposed reference URL for %q in %s", title, band.Name)
	default:
		what = fmt.Sprintf("your proposal in %s", band.Name)
	}

	var body string
	switch newStatus {
	case "accepted":
		body = fmt.Sprintf("Accepted: %s", what)
	case "rejected":
		body = fmt.Sprintf("Rejected: %s", what)
	default:
		body = fmt.Sprintf("Decided (%s): %s", newStatus, what)
	}

	link := fmt.Sprintf("/bands/%d/proposals/%d", p.BandID, p.ID)
	_, err = st.CreateNotification(ctx, p.ProposerPersonID, "proposal_"+newStatus, body, link)
	return err
}
