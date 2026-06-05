// Fallback canonical amenity codes, mirroring the backend's
// property.CanonicalAmenities. The live list is fetched from GET /amenities;
// this is used as the initial value and a fallback if the request fails.
export const AMENITY_CODES = [
  'wifi',
  'kitchen',
  'parking',
  'pool',
  'air-conditioning',
  'heating',
  'washer',
  'dryer',
  'tv',
  'workspace',
  'hot-tub',
  'gym',
  'pets-allowed',
  'elevator',
  'breakfast',
  'smoke-alarm',
  // Accessibility codes (S161) — must stay in sync with backend
  // property.CanonicalAmenities so the EAA / ADA filter options render even
  // when GET /amenities fails and we fall back to this client-side list.
  'wheelchair-accessible',
  'step-free-entry',
  'wide-doorways',
  'roll-in-shower',
  'accessible-parking',
  'accessible-bathroom',
];
