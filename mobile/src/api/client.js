import { API_BASE_URL } from '../config';

// createApi builds an API client bound to an access-token provider, so screens
// stay free of auth plumbing. The same endpoints as the web client are exposed.
export function createApi(getAccessToken) {
  async function request(method, path, { body, auth = false } = {}) {
    const headers = {};
    const opts = { method, headers };

    if (auth) {
      const token = await getAccessToken();
      if (token) headers.Authorization = `Bearer ${token}`;
    }
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }

    const res = await fetch(`${API_BASE_URL}${path}`, opts);
    if (res.status === 204) return null;
    const text = await res.text();
    const data = text ? JSON.parse(text) : null;
    if (!res.ok) {
      const err = new Error((data && data.error) || res.statusText);
      if (data) {
        if (data.code) err.code = data.code;
        if (data.details) err.details = data.details;
      }
      err.status = res.status;
      throw err;
    }
    return data;
  }

  // upload sends a multipart/form-data file (from an image/document picker).
  // React Native's fetch sets the multipart boundary itself, so we must NOT set
  // Content-Type manually.
  async function upload(path, field, file) {
    const token = await getAccessToken();
    const fd = new FormData();
    fd.append(field, { uri: file.uri, name: file.name || 'upload.jpg', type: file.type || 'image/jpeg' });
    const res = await fetch(`${API_BASE_URL}${path}`, {
      method: 'POST',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      body: fd,
    });
    const text = await res.text();
    const data = text ? JSON.parse(text) : null;
    if (!res.ok) throw new Error((data && data.error) || res.statusText);
    return data;
  }

  return {
    searchProperties: (params = {}) => {
      const qs = new URLSearchParams(params).toString();
      return request('GET', `/properties${qs ? `?${qs}` : ''}`);
    },
    getProperty: (id) => request('GET', `/properties/${id}`),
    // Experiences — public read endpoints (S79 mobile parity with web S76).
    // Search filters: category, city, language, limit, offset.
    searchExperiences: (params = {}) => {
      const qs = new URLSearchParams(params).toString();
      return request('GET', `/experiences${qs ? `?${qs}` : ''}`);
    },
    getExperience: (id) => request('GET', `/experiences/${id}`),
    // ExperienceBooking (S84 mobile parity with backend S80) — guests book a
    // session of an experience, list/view/cancel their own bookings.
    createExperienceBooking: (expId, body) =>
      request('POST', `/experiences/${expId}/bookings`, { body, auth: true }),
    myExperienceBookings: () => request('GET', '/experience-bookings/me', { auth: true }),
    getExperienceBooking: (id) => request('GET', `/experience-bookings/${id}`, { auth: true }),
    cancelExperienceBooking: (id) =>
      request('POST', `/experience-bookings/${id}/cancel`, { auth: true }),
    // Saved searches & alerts
    listSavedSearches: () => request('GET', '/saved-searches', { auth: true }),
    saveSearch: (body) => request('POST', '/saved-searches', { body, auth: true }),
    deleteSavedSearch: (id) => request('DELETE', `/saved-searches/${id}`, { auth: true }),
    getReviews: (id) => request('GET', `/properties/${id}/reviews`),
    getReviewSummary: (id) => request('GET', `/properties/${id}/reviews/summary`),
    // House rules (S64 mobile parity). Public read — anonymous-friendly
    // so the rules render alongside the listing without forcing a login.
    // setHouseRules (S95 mobile parity with web S56) — host-only PATCH
    // that replaces the full rule set and bumps the version on the server.
    // Clients send the complete intent (not a patch) because the
    // versioned-history schema makes incremental edits unsafe.
    getHouseRules: (id) => request('GET', `/properties/${id}/house-rules`),
    setHouseRules: (id, items) =>
      request('PATCH', `/properties/${id}/house-rules`, { body: { items }, auth: true }),
    // Tax quote — public preview of the per-jurisdiction tax breakdown
    // (S64 mobile parity). Anonymous-friendly so the booking screen can
    // render lines before sign-in.
    getTaxQuote: (id, { checkIn, nights, guests, subtotalCents }) => {
      const qs = new URLSearchParams({
        checkIn, nights: String(nights), guests: String(guests), subtotalCents: String(subtotalCents),
      }).toString();
      return request('GET', `/properties/${id}/tax-quote?${qs}`);
    },
    me: () => request('GET', '/me', { auth: true }),
    becomeHost: () => request('POST', '/me/become-host', { auth: true }),
    updatePreferences: (prefs) => request('PATCH', '/me/preferences', { body: prefs, auth: true }),
    // exportMyData (S95 mobile parity with web S109) — GDPR right of
    // access. Server returns the full personal-data bundle as JSON; we
    // hand the parsed object back to the caller so it can serialize and
    // share/copy as appropriate for the platform.
    exportMyData: () => request('GET', '/me/export', { auth: true }),
    deleteAccount: () => request('DELETE', '/me', { auth: true }),

    // Push notification device registration
    listPushTokens: () => request('GET', '/me/push-tokens', { auth: true }),
    registerPushToken: (platform, token) =>
      request('POST', '/me/push-tokens', { body: { platform, token }, auth: true }),
    unregisterPushToken: (platform, token) =>
      request('POST', '/me/push-tokens/unregister', { body: { platform, token }, auth: true }),

    // Identity verification (KYC)
    getVerification: () => request('GET', '/me/verification', { auth: true }),
    submitVerification: (body) => request('POST', '/me/verification', { body, auth: true }),

    // Blocking users
    listUserBlocks: () => request('GET', '/me/blocks', { auth: true }),
    blockUser: (userId) => request('POST', `/users/${userId}/block`, { auth: true }),
    unblockUser: (userId) => request('DELETE', `/users/${userId}/block`, { auth: true }),

    createBooking: (body) => request('POST', '/bookings', { body, auth: true }),
    previewCoupon: (body) => request('POST', '/bookings/preview-coupon', { body, auth: true }),
    myBookings: () => request('GET', '/bookings/me', { auth: true }),
    myOffers: () => request('GET', '/offers', { auth: true }),
    acceptOffer: (id) => request('POST', `/offers/${id}/accept`, { auth: true }),
    declineOffer: (id) => request('POST', `/offers/${id}/decline`, { auth: true }),
    modifyBooking: (id, body) => request('POST', `/bookings/${id}/modify`, { body, auth: true }),
    cancelBooking: (id) => request('POST', `/bookings/${id}/cancel`, { auth: true }),

    // Post-stay reviews
    pendingReviews: () => request('GET', '/me/reviews/pending', { auth: true }),
    createReview: (body) => request('POST', '/reviews', { body, auth: true }),
    editReview: (id, body) => request('PATCH', `/reviews/${id}`, { body, auth: true }),
    deleteReview: (id) => request('DELETE', `/reviews/${id}`, { auth: true }),
    respondToReview: (reviewId, response) => request('POST', `/reviews/${reviewId}/response`, { body: { response }, auth: true }),
    createGuestReview: (body) => request('POST', '/reviews/guest', { body, auth: true }),
    myGuestReviews: () => request('GET', '/me/guest-reviews', { auth: true }),
    reportReview: (reviewId, body) => request('POST', `/reviews/${reviewId}/reports`, { body, auth: true }),

    // Payments (guest-facing reads)
    listPayments: () => request('GET', '/payments/me', { auth: true }),
    getBookingDeposit: (bookingId) => request('GET', `/bookings/${bookingId}/deposit`, { auth: true }),
    getBookingArrival: (bookingId) => request('GET', `/bookings/${bookingId}/arrival`, { auth: true }),

    // Report a listing for moderation
    reportListing: (propertyId, body) => request('POST', `/properties/${propertyId}/reports`, { body, auth: true }),

    // Resolution Center
    openDispute: (bookingId, body) => request('POST', `/bookings/${bookingId}/disputes`, { body, auth: true }),
    listMyDisputes: () => request('GET', '/me/disputes', { auth: true }),
    getDispute: (id) => request('GET', `/disputes/${id}`, { auth: true }),
    addDisputeEvidence: (id, body) => request('POST', `/disputes/${id}/evidence`, { body, auth: true }),
    hostRespondDispute: (id, response) =>
      request('POST', `/disputes/${id}/host-response`, { body: { response }, auth: true }),
    // postDisputeReply (S129 mobile) — a "reply" to an open dispute is a
    // free-text body plus an optional list of photo evidence URLs. The
    // backend has no batched /replies endpoint; the closest primitive is
    // POST /disputes/:id/evidence, which already records each {url, note}
    // pair on the case timeline. We model a reply by fanning the user's
    // single submit out across that primitive: if photo URLs are present
    // we post one evidence per URL (each carrying the body as its note,
    // so screen readers and exports show the same context next to each
    // photo); otherwise we post one note-only evidence. The final server
    // response (the updated dispute) is returned so the caller can
    // refresh its in-memory timeline without an extra GET.
    postDisputeReply: async (id, { body, photoUrls = [] }) => {
      const text = (body || '').trim();
      const urls = (photoUrls || []).map((u) => u.trim()).filter(Boolean);
      let last = null;
      if (urls.length === 0) {
        last = await request('POST', `/disputes/${id}/evidence`, {
          body: { url: '', note: text },
          auth: true,
        });
      } else {
        for (const url of urls) {
          // eslint-disable-next-line no-await-in-loop
          last = await request('POST', `/disputes/${id}/evidence`, {
            body: { url, note: text },
            auth: true,
          });
        }
      }
      return last;
    },

    // Host
    hostMetrics: () => request('GET', '/host/metrics', { auth: true }),
    hostEarnings: () => request('GET', '/host/earnings', { auth: true }),
    payoutAvailable: () => request('GET', '/host/payouts/available', { auth: true }),
    listDisbursements: () => request('GET', '/host/payouts', { auth: true }),
    requestPayout: (currency) => request('POST', '/host/payouts', { body: { currency }, auth: true }),
    onboardPayouts: (body) => request('POST', '/host/payouts/onboard', { body, auth: true }),
    refreshPayoutAccount: () => request('POST', '/host/payouts/account/refresh', { auth: true }),
    myProperties: () => request('GET', '/host/properties', { auth: true }),
    propertyBookings: (id) => request('GET', `/properties/${id}/bookings`, { auth: true }),

    // Listing management (host)
    createProperty: (body) => request('POST', '/properties', { body, auth: true }),
    updateProperty: (id, body) => request('PATCH', `/properties/${id}`, { body, auth: true }),
    publishProperty: (id) => request('POST', `/properties/${id}/publish`, { auth: true }),
    deleteProperty: (id) => request('DELETE', `/properties/${id}`, { auth: true }),
    // duplicateProperty (S60 backend, S67 mobile parity) — clones an owned
    // listing into a fresh draft. Photos and arrival info don't carry over;
    // the new id is returned so the caller can navigate to its edit form.
    duplicateProperty: (id) => request('POST', `/properties/${id}/duplicate`, { auth: true }),
    uploadPhoto: (id, file) => upload(`/properties/${id}/photos`, 'photo', file),
    // reorderPhotos (S95 mobile parity with web S60) — replaces the
    // listing's photo order. The first id becomes the cover. Server
    // returns the full property so callers can drop the response
    // straight into their existing photo grid state.
    reorderPhotos: (id, photoIds) =>
      request('PATCH', `/properties/${id}/photos/order`, { body: { photoIds }, auth: true }),
    deletePhoto: (id, photoId) => request('DELETE', `/properties/${id}/photos/${photoId}`, { auth: true }),

    // Calendar blocks (host)
    listBlocks: (propertyId) => request('GET', `/properties/${propertyId}/blocks`, { auth: true }),
    createBlock: (propertyId, body) => request('POST', `/properties/${propertyId}/blocks`, { body, auth: true }),
    deleteBlock: (blockId) => request('DELETE', `/blocks/${blockId}`, { auth: true }),
    importCalendar: (propertyId, ical) => request('POST', `/properties/${propertyId}/calendar/import`, { body: { ical }, auth: true }),

    // Per-date price overrides (seasonal pricing).
    listPriceRules: (propertyId) => request('GET', `/properties/${propertyId}/price-rules`, { auth: true }),
    createPriceRule: (propertyId, body) => request('POST', `/properties/${propertyId}/price-rules`, { body, auth: true }),
    deletePriceRule: (propertyId, ruleId) => request('DELETE', `/properties/${propertyId}/price-rules/${ruleId}`, { auth: true }),

    // Co-host grants (read-only on mobile: surface "listings I help manage").
    myCohostListings: () => request('GET', '/me/cohost-listings', { auth: true }),
    myCohostMailbox: () => request('GET', '/me/cohost-mailbox', { auth: true }),

    // Co-host invitation accept/decline (S125 mobile parity). Each grant
    // created via inviteCohost is initially "invited" — the invitee taps a
    // push notification (cohost.invited) which deep-links into the
    // CohostInvitationScreen, and accepts or declines from there. The URL
    // shape mirrors offers (POST /offers/:id/accept, /decline) since they
    // are the same product pattern (invitation → bilateral action).
    acceptCohostInvitation: (invitationId) =>
      request('POST', `/cohost-invitations/${invitationId}/accept`, { auth: true }),
    declineCohostInvitation: (invitationId) =>
      request('POST', `/cohost-invitations/${invitationId}/decline`, { auth: true }),

    // Primary-host editor (S104 mobile parity with web's CohostsPanel).
    // The host lists, invites and revokes co-hosts on listings they own;
    // updating an existing grant's permission set replaces the full set
    // (matches the backend's PATCH semantics — empty sets are rejected).
    // Route paths sit under /host/properties/:id/cohosts to match the
    // backend router and the existing web client.
    listCohosts: (propertyId) =>
      request('GET', `/host/properties/${propertyId}/cohosts`, { auth: true }),
    inviteCohost: (propertyId, body) =>
      request('POST', `/host/properties/${propertyId}/cohosts`, { body, auth: true }),
    updateCohostPermissions: (propertyId, cohostId, permissions) =>
      request('PATCH', `/host/properties/${propertyId}/cohosts/${cohostId}`, {
        body: { permissions },
        auth: true,
      }),
    revokeCohost: (propertyId, cohostId) =>
      request('DELETE', `/host/properties/${propertyId}/cohosts/${cohostId}`, { auth: true }),

    // Split payment between travellers (read + authorize + cancel on mobile).
    mySplits: () => request('GET', '/me/splits', { auth: true }),
    getSplit: (id) => request('GET', `/splits/${id}`, { auth: true }),
    getBookingSplit: (bookingId) => request('GET', `/bookings/${bookingId}/split`, { auth: true }),
    authorizeShare: (splitId, shareId) =>
      request('POST', `/splits/${splitId}/shares/${shareId}/authorize`, { auth: true }),
    cancelSplit: (splitId) => request('POST', `/splits/${splitId}/cancel`, { auth: true }),

    // Saved-reply templates (host playbook surfaced in the composer).
    listMessageTemplates: () => request('GET', '/me/message-templates', { auth: true }),
    createMessageTemplate: (body) => request('POST', '/me/message-templates', { body, auth: true }),
    updateMessageTemplate: (id, body) => request('PATCH', `/me/message-templates/${id}`, { body, auth: true }),
    deleteMessageTemplate: (id) => request('DELETE', `/me/message-templates/${id}`, { auth: true }),

    confirmBooking: (id) => request('POST', `/bookings/${id}/confirm`, { auth: true }),
    completeBooking: (id) => request('POST', `/bookings/${id}/complete`, { auth: true }),

    // Favorites & wishlist collections
    listFavorites: (collectionId) =>
      request('GET', `/favorites${collectionId ? `?collectionId=${encodeURIComponent(collectionId)}` : ''}`, { auth: true }),
    addFavorite: (propertyId, collectionId) => request('POST', '/favorites', { body: { propertyId, collectionId }, auth: true }),
    removeFavorite: (propertyId) => request('DELETE', `/favorites/${propertyId}`, { auth: true }),
    moveFavorite: (propertyId, collectionId) => request('PATCH', `/favorites/${propertyId}`, { body: { collectionId: collectionId || '' }, auth: true }),
    listCollections: () => request('GET', '/wishlist/collections', { auth: true }),
    createCollection: (name) => request('POST', '/wishlist/collections', { body: { name }, auth: true }),
    deleteCollection: (id) => request('DELETE', `/wishlist/collections/${id}`, { auth: true }),
    shareCollection: (id) => request('POST', `/wishlist/collections/${id}/share`, { auth: true }),
    unshareCollection: (id) => request('DELETE', `/wishlist/collections/${id}/share`, { auth: true }),

    // In-app notifications
    listNotifications: () => request('GET', '/notifications', { auth: true }),
    markNotificationRead: (id) => request('POST', `/notifications/${id}/read`, { auth: true }),
    markNotificationUnread: (id) => request('POST', `/notifications/${id}/unread`, { auth: true }),
    markAllNotificationsRead: () => request('POST', '/notifications/read-all', { auth: true }),

    // Messaging
    listConversations: () => request('GET', '/conversations', { auth: true }),
    startConversation: (propertyId) => request('POST', '/conversations', { body: { propertyId }, auth: true }),
    listMessages: (id) => request('GET', `/conversations/${id}/messages`, { auth: true }),
    sendMessage: (id, body) => request('POST', `/conversations/${id}/messages`, { body: { body }, auth: true }),
    sendAttachment: (id, file) => upload(`/conversations/${id}/attachments`, 'file', file),
    markConversationRead: (id) => request('POST', `/conversations/${id}/read`, { auth: true }),
  };
}
