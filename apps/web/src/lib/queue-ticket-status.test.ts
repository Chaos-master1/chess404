// @vitest-environment node
import { describe, expect, it } from 'vitest';

import { displayTicketStatus, isSeekingStatus } from './queue-ticket-status';

// Production defect (2026-10-03): a poll that landed inside the matchmaking
// service's pairing window received the internal status 'pairing', the queue
// stopped polling (only 'queued' was fetched) and the auto-open effect never
// fired (it requires 'matched'), parking the player on "Matched - opening
// game..." forever. These helpers are what the queue branches on.
describe('queue ticket status', () => {
  describe('isSeekingStatus', () => {
    it('keeps polling through both queued and the pairing reservation', () => {
      expect(isSeekingStatus('queued')).toBe(true);
      expect(isSeekingStatus('pairing')).toBe(true);
    });

    it('stops on terminal statuses', () => {
      expect(isSeekingStatus('matched')).toBe(false);
      expect(isSeekingStatus('cancelled')).toBe(false);
    });

    it('treats absent statuses as not seeking', () => {
      expect(isSeekingStatus(undefined)).toBe(false);
      expect(isSeekingStatus(null)).toBe(false);
      expect(isSeekingStatus('')).toBe(false);
    });
  });

  describe('displayTicketStatus', () => {
    it('presents the internal pairing reservation as a plain queued ticket', () => {
      expect(displayTicketStatus('pairing')).toBe('queued');
    });

    it('passes terminal statuses through unchanged', () => {
      expect(displayTicketStatus('queued')).toBe('queued');
      expect(displayTicketStatus('matched')).toBe('matched');
      expect(displayTicketStatus('cancelled')).toBe('cancelled');
    });

    it('falls back to idle when no ticket exists', () => {
      expect(displayTicketStatus(undefined)).toBe('idle');
      expect(displayTicketStatus(null)).toBe('idle');
    });
  });
});
