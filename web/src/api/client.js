import keycloak from '../keycloak';

const BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8081/api/v1';

// request is a thin fetch wrapper that attaches the bearer token (refreshing it
// when close to expiry) and normalises error handling.
async function request(method, path, { body, formData, auth = false } = {}) {
  const headers = {};
  const opts = { method, headers };

  if (auth && keycloak.authenticated) {
    try {
      await keycloak.updateToken(30);
    } catch {
      keycloak.login();
      return;
    }
    headers.Authorization = `Bearer ${keycloak.token}`;
  }

  if (formData) {
    opts.body = formData;
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }

  const res = await fetch(`${BASE_URL}${path}`, opts);
  if (res.status === 204) return null;

  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const message = (data && data.error) || res.statusText;
    // Attach the structured error envelope to the thrown Error so callers
    // can route on the typed code (e.g. "kyc_step_up_required") without
    // parsing the message string. Plain `e.message` continues to work for
    // generic surfaces that don't care.
    const err = new Error(message);
    if (data) {
      if (data.code) err.code = data.code;
      if (data.details) err.details = data.details;
    }
    err.status = res.status;
    throw err;
  }
  return data;
}

// downloadFile fetches an authenticated binary endpoint and triggers a browser
// download, since a plain link cannot carry the bearer token.
async function downloadFile(path, filename) {
  if (!keycloak.authenticated) {
    keycloak.login();
    return;
  }
  try {
    await keycloak.updateToken(30);
  } catch {
    keycloak.login();
    return;
  }
  const res = await fetch(`${BASE_URL}${path}`, {
    headers: { Authorization: `Bearer ${keycloak.token}` },
  });
  if (!res.ok) throw new Error(`Download failed (${res.status})`);
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

// buildQuery turns an object into a query string, repeating the key for array
// values (e.g. { amenity: ['wifi','tv'] } -> "amenity=wifi&amenity=tv").
function buildQuery(params) {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (Array.isArray(v)) v.forEach((item) => qs.append(k, item));
    else qs.append(k, v);
  }
  return qs.toString();
}

export const api = {
  // Listings (public)
  searchProperties: (params = {}) => {
    const qs = buildQuery(params);
    return request('GET', `/properties${qs ? `?${qs}` : ''}`);
  },
  getProperty: (id) => request('GET', `/properties/${id}`),
  listAmenities: () => request('GET', '/amenities'),
  // House rules (S47 + S56) — public read of the current versioned rule
  // set, host-only PATCH that bumps version (history preserved server-side).
  // getHouseRulesAcceptance returns the per-booking proof bundled with the
  // exact items the guest acknowledged.
  getHouseRules: (propertyId) => request('GET', `/properties/${propertyId}/house-rules`),
  setHouseRules: (propertyId, items) =>
    request('PATCH', `/properties/${propertyId}/house-rules`, { body: { items }, auth: true }),
  getHouseRulesAcceptance: (bookingId) =>
    request('GET', `/bookings/${bookingId}/house-rules-acceptance`, { auth: true }),
  // Per-jurisdiction tax preview (S48 + S57). Public — anonymous
  // browsers can render the breakdown before sign-in. The backend
  // resolves the listing's (country, city, currency) so the caller
  // only sends the stay shape.
  getTaxQuote: (propertyId, { checkIn, nights, guests, subtotalCents }) =>
    request(
      'GET',
      `/properties/${propertyId}/tax-quote?` +
        new URLSearchParams({
          checkIn,
          nights: String(nights),
          guests: String(guests),
          subtotalCents: String(subtotalCents),
        }).toString(),
    ),
  // Saved searches & alerts
  listSavedSearches: () => request('GET', '/saved-searches', { auth: true }),
  saveSearch: (body) => request('POST', '/saved-searches', { body, auth: true }),
  deleteSavedSearch: (id) => request('DELETE', `/saved-searches/${id}`, { auth: true }),
  setSearchAlerts: (id, alertsEnabled) => request('PATCH', `/saved-searches/${id}`, { body: { alertsEnabled }, auth: true }),
  getReviews: (id) => request('GET', `/properties/${id}/reviews`),
  getReviewSummary: (id) => request('GET', `/properties/${id}/reviews/summary`),

  // Profile
  me: () => request('GET', '/me', { auth: true }),
  updateProfile: (body) => request('PATCH', '/me', { body, auth: true }),
  updatePreferences: (prefs) => request('PATCH', '/me/preferences', { body: prefs, auth: true }),
  becomeHost: () => request('POST', '/me/become-host', { auth: true }),

  // Push notification device registration (Web Push / mobile native).
  listPushTokens: () => request('GET', '/me/push-tokens', { auth: true }),
  registerPushToken: (platform, token) =>
    request('POST', '/me/push-tokens', { body: { platform, token }, auth: true }),
  unregisterPushToken: (platform, token) =>
    request('POST', '/me/push-tokens/unregister', { body: { platform, token }, auth: true }),

  // GDPR self-service
  exportMyData: () => downloadFile('/me/export', 'airhost-data-export.json'),
  deleteAccount: () => request('DELETE', '/me', { auth: true }),

  // Identity verification (KYC)
  getVerification: () => request('GET', '/me/verification', { auth: true }),
  submitVerification: (body) => request('POST', '/me/verification', { body, auth: true }),

  // Listing reports
  reportListing: (propertyId, body) => request('POST', `/properties/${propertyId}/reports`, { body, auth: true }),

  // Resolution Center
  openDispute: (bookingId, body) => request('POST', `/bookings/${bookingId}/disputes`, { body, auth: true }),
  listMyDisputes: () => request('GET', '/me/disputes', { auth: true }),
  getDispute: (id) => request('GET', `/disputes/${id}`, { auth: true }),
  addDisputeEvidence: (id, body) => request('POST', `/disputes/${id}/evidence`, { body, auth: true }),
  hostRespondDispute: (id, response) => request('POST', `/disputes/${id}/host-response`, { body: { response }, auth: true }),
  adminListOpenDisputes: () => request('GET', '/admin/disputes', { auth: true }),
  // adminResolveDispute accepts either a plain resolution string (legacy) or
  // a body object { resolution, refundAmountCents, damageAmountCents } so the
  // moderator can attach a partial refund or a damage claim to the decision.
  adminResolveDispute: (id, body) =>
    request('POST', `/admin/disputes/${id}/resolve`,
      { body: typeof body === 'string' ? { resolution: body } : body, auth: true }),
  adminRejectDispute: (id, body) =>
    request('POST', `/admin/disputes/${id}/reject`,
      { body: typeof body === 'string' ? { resolution: body } : body, auth: true }),

  // Admin moderation
  adminListVerifications: () => request('GET', '/admin/verifications', { auth: true }),
  adminApproveVerification: (id) => request('POST', `/admin/verifications/${id}/approve`, { auth: true }),
  adminRejectVerification: (id, reason) => request('POST', `/admin/verifications/${id}/reject`, { body: { reason }, auth: true }),
  adminListReports: () => request('GET', '/admin/reports', { auth: true }),
  adminResolveReport: (id, resolution) => request('POST', `/admin/reports/${id}/resolve`, { body: { resolution }, auth: true }),
  adminDismissReport: (id, resolution) => request('POST', `/admin/reports/${id}/dismiss`, { body: { resolution }, auth: true }),
  adminSuspendProperty: (id) => request('POST', `/admin/properties/${id}/suspend`, { auth: true }),
  adminUnsuspendProperty: (id) => request('POST', `/admin/properties/${id}/unsuspend`, { auth: true }),

  // Promo codes (admin)
  adminListCoupons: () => request('GET', '/admin/coupons', { auth: true }),
  adminCreateCoupon: (body) => request('POST', '/admin/coupons', { body, auth: true }),
  adminDeactivateCoupon: (id) => request('POST', `/admin/coupons/${id}/deactivate`, { auth: true }),

  // Live alert state (firing + recently resolved), pushed by Alertmanager
  adminListAlerts: () => request('GET', '/admin/alerts', { auth: true }),

  // Alertmanager silences (maintenance windows that mute alerts)
  adminListSilences: () => request('GET', '/admin/alerts/silences', { auth: true }),
  adminCreateSilence: (body) => request('POST', '/admin/alerts/silences', { body, auth: true }),
  adminDeleteSilence: (id) => request('DELETE', `/admin/alerts/silences/${id}`, { auth: true }),

  // Bookings
  createBooking: (body) => request('POST', '/bookings', { body, auth: true }),
  previewCoupon: (body) => request('POST', '/bookings/preview-coupon', { body, auth: true }),
  myBookings: () => request('GET', '/bookings/me', { auth: true }),
  // Offers (host pre-approval / special offer)
  myOffers: () => request('GET', '/offers', { auth: true }),
  acceptOffer: (id) => request('POST', `/offers/${id}/accept`, { auth: true }),
  declineOffer: (id) => request('POST', `/offers/${id}/decline`, { auth: true }),
  sendOffer: (body) => request('POST', '/offers', { body, auth: true }),
  modifyBooking: (id, body) => request('POST', `/bookings/${id}/modify`, { body, auth: true }),
  cancelBooking: (id) => request('POST', `/bookings/${id}/cancel`, { auth: true }),

  // Reviews
  createReview: (body) => request('POST', '/reviews', { body, auth: true }),
  editReview: (id, body) => request('PATCH', `/reviews/${id}`, { body, auth: true }),
  deleteReview: (id) => request('DELETE', `/reviews/${id}`, { auth: true }),
  respondToReview: (reviewId, response) => request('POST', `/reviews/${reviewId}/response`, { body: { response }, auth: true }),
  createGuestReview: (body) => request('POST', '/reviews/guest', { body, auth: true }),
  myGuestReviews: () => request('GET', '/me/guest-reviews', { auth: true }),
  myPendingReviews: () => request('GET', '/me/reviews/pending', { auth: true }),
  reportReview: (reviewId, body) => request('POST', `/reviews/${reviewId}/reports`, { body, auth: true }),

  // Blocking users
  listUserBlocks: () => request('GET', '/me/blocks', { auth: true }),
  blockUser: (userId) => request('POST', `/users/${userId}/block`, { auth: true }),
  unblockUser: (userId) => request('DELETE', `/users/${userId}/block`, { auth: true }),

  // Host
  hostMetrics: () => request('GET', '/host/metrics', { auth: true }),
  hostEarnings: () => request('GET', '/host/earnings', { auth: true }),
  hostEarningEntries: () => request('GET', '/host/earnings/entries', { auth: true }),
  downloadEarningsCsv: () => downloadFile('/host/earnings/export.csv', 'airhost-earnings.csv'),
  payoutAvailable: () => request('GET', '/host/payouts/available', { auth: true }),
  listDisbursements: () => request('GET', '/host/payouts', { auth: true }),
  requestPayout: (currency) => request('POST', '/host/payouts', { body: { currency }, auth: true }),
  onboardPayouts: (body) => request('POST', '/host/payouts/onboard', { body, auth: true }),
  refreshPayoutAccount: () => request('POST', '/host/payouts/account/refresh', { auth: true }),
  myProperties: () => request('GET', '/host/properties', { auth: true }),
  // getHostProperty returns the listing with the host-only arrival block —
  // use this on the edit form so the host sees the credentials they stored.
  getHostProperty: (id) => request('GET', `/host/properties/${id}`, { auth: true }),
  createProperty: (body) => request('POST', '/properties', { body, auth: true }),
  updateProperty: (id, body) => request('PATCH', `/properties/${id}`, { body, auth: true }),
  publishProperty: (id) => request('POST', `/properties/${id}/publish`, { auth: true }),
  deleteProperty: (id) => request('DELETE', `/properties/${id}`, { auth: true }),
  uploadPhoto: (id, file) => {
    const fd = new FormData();
    fd.append('photo', file);
    return request('POST', `/properties/${id}/photos`, { formData: fd, auth: true });
  },
  reorderPhotos: (id, photoIds) => request('PATCH', `/properties/${id}/photos/order`, { body: { photoIds }, auth: true }),
  deletePhoto: (id, photoId) => request('DELETE', `/properties/${id}/photos/${photoId}`, { auth: true }),
  propertyBookings: (id) => request('GET', `/properties/${id}/bookings`, { auth: true }),
  confirmBooking: (id) => request('POST', `/bookings/${id}/confirm`, { auth: true }),
  completeBooking: (id) => request('POST', `/bookings/${id}/complete`, { auth: true }),
  listBlocks: (propertyId) => request('GET', `/properties/${propertyId}/blocks`, { auth: true }),
  createBlock: (propertyId, body) => request('POST', `/properties/${propertyId}/blocks`, { body, auth: true }),
  deleteBlock: (blockId) => request('DELETE', `/blocks/${blockId}`, { auth: true }),
  importCalendar: (propertyId, ical) => request('POST', `/properties/${propertyId}/calendar/import`, { body: { ical }, auth: true }),
  calendarFeedUrl: (propertyId) => `${BASE_URL}/properties/${propertyId}/calendar.ics`,

  // Per-date price overrides (seasonal pricing).
  listPriceRules: (propertyId) => request('GET', `/properties/${propertyId}/price-rules`, { auth: true }),
  createPriceRule: (propertyId, body) => request('POST', `/properties/${propertyId}/price-rules`, { body, auth: true }),
  deletePriceRule: (propertyId, ruleId) => request('DELETE', `/properties/${propertyId}/price-rules/${ruleId}`, { auth: true }),

  // Co-hosts (primary host invites and revokes; the cohost lists their listings).
  listCohosts: (propertyId) => request('GET', `/host/properties/${propertyId}/cohosts`, { auth: true }),
  inviteCohost: (propertyId, body) => request('POST', `/host/properties/${propertyId}/cohosts`, { body, auth: true }),
  updateCohostPermissions: (propertyId, cohostId, permissions) =>
    request('PATCH', `/host/properties/${propertyId}/cohosts/${cohostId}`, { body: { permissions }, auth: true }),
  revokeCohost: (propertyId, cohostId) =>
    request('DELETE', `/host/properties/${propertyId}/cohosts/${cohostId}`, { auth: true }),
  myCohostListings: () => request('GET', '/me/cohost-listings', { auth: true }),
  // The "team mailbox": conversations on listings where I'm a co-host with
  // reply_messages. Distinct from /conversations (those are my own threads).
  myCohostMailbox: () => request('GET', '/me/cohost-mailbox', { auth: true }),

  // Split payment between travellers.
  mySplits: () => request('GET', '/me/splits', { auth: true }),
  getSplit: (id) => request('GET', `/splits/${id}`, { auth: true }),
  getBookingSplit: (bookingId) => request('GET', `/bookings/${bookingId}/split`, { auth: true }),
  authorizeShare: (splitId, shareId) =>
    request('POST', `/splits/${splitId}/shares/${shareId}/authorize`, { auth: true }),
  cancelSplit: (splitId) => request('POST', `/splits/${splitId}/cancel`, { auth: true }),

  // Saved-reply templates (per-user playbook surfaced in the messaging composer).
  listMessageTemplates: () => request('GET', '/me/message-templates', { auth: true }),
  createMessageTemplate: (body) => request('POST', '/me/message-templates', { body, auth: true }),
  updateMessageTemplate: (id, body) => request('PATCH', `/me/message-templates/${id}`, { body, auth: true }),
  deleteMessageTemplate: (id) => request('DELETE', `/me/message-templates/${id}`, { auth: true }),

  // Availability (public)
  availability: (id, params = {}) => {
    const qs = new URLSearchParams(params).toString();
    return request('GET', `/properties/${id}/availability${qs ? `?${qs}` : ''}`);
  },

  // Messaging
  startConversation: (propertyId) => request('POST', '/conversations', { body: { propertyId }, auth: true }),
  listConversations: () => request('GET', '/conversations', { auth: true }),
  listMessages: (id) => request('GET', `/conversations/${id}/messages`, { auth: true }),
  sendMessage: (id, body) => request('POST', `/conversations/${id}/messages`, { body: { body }, auth: true }),
  sendAttachment: (id, file, body = '') => {
    const fd = new FormData();
    fd.append('file', file);
    if (body) fd.append('body', body);
    return request('POST', `/conversations/${id}/attachments`, { formData: fd, auth: true });
  },
  markConversationRead: (id) => request('POST', `/conversations/${id}/read`, { auth: true }),
  messagesUnreadCount: () => request('GET', '/conversations/unread-count', { auth: true }),

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
  getSharedCollection: (token) => request('GET', `/shared/collections/${encodeURIComponent(token)}`),

  // Notifications
  listNotifications: () => request('GET', '/notifications', { auth: true }),
  markNotificationRead: (id) => request('POST', `/notifications/${id}/read`, { auth: true }),
  markNotificationUnread: (id) => request('POST', `/notifications/${id}/unread`, { auth: true }),
  markAllNotificationsRead: () => request('POST', '/notifications/read-all', { auth: true }),

  // Payments
  listPayments: () => request('GET', '/payments/me', { auth: true }),
  downloadReceipt: (bookingId) => downloadFile(`/bookings/${bookingId}/receipt`, `airhost-receipt-${bookingId}.pdf`),
  getBookingDeposit: (bookingId) => request('GET', `/bookings/${bookingId}/deposit`, { auth: true }),
  // getBookingArrival returns the listing's check-in info to the booking's
  // guest within the 48h reveal window (403 outside it, 404 when the listing
  // has no arrival info configured).
  getBookingArrival: (bookingId) => request('GET', `/bookings/${bookingId}/arrival`, { auth: true }),
};
