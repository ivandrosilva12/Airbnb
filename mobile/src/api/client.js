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
    if (!res.ok) throw new Error((data && data.error) || res.statusText);
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
    getReviews: (id) => request('GET', `/properties/${id}/reviews`),
    getReviewSummary: (id) => request('GET', `/properties/${id}/reviews/summary`),
    me: () => request('GET', '/me', { auth: true }),
    becomeHost: () => request('POST', '/me/become-host', { auth: true }),
    updatePreferences: (prefs) => request('PATCH', '/me/preferences', { body: prefs, auth: true }),
    deleteAccount: () => request('DELETE', '/me', { auth: true }),

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

    // Report a listing for moderation
    reportListing: (propertyId, body) => request('POST', `/properties/${propertyId}/reports`, { body, auth: true }),

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
    uploadPhoto: (id, file) => upload(`/properties/${id}/photos`, 'photo', file),
    deletePhoto: (id, photoId) => request('DELETE', `/properties/${id}/photos/${photoId}`, { auth: true }),

    // Calendar blocks (host)
    listBlocks: (propertyId) => request('GET', `/properties/${propertyId}/blocks`, { auth: true }),
    createBlock: (propertyId, body) => request('POST', `/properties/${propertyId}/blocks`, { body, auth: true }),
    deleteBlock: (blockId) => request('DELETE', `/blocks/${blockId}`, { auth: true }),
    importCalendar: (propertyId, ical) => request('POST', `/properties/${propertyId}/calendar/import`, { body: { ical }, auth: true }),
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
