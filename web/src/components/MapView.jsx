import { useEffect, useRef } from 'react';
import L from 'leaflet';
import 'leaflet/dist/leaflet.css';

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}

// MapView renders search results as markers on an OpenStreetMap (Leaflet) map.
// It uses vector circle markers so no marker-image assets are required, and
// fits the viewport to the plotted listings.
export default function MapView({ properties, onSelect }) {
  const containerRef = useRef(null);
  const mapRef = useRef(null);
  const layerRef = useRef(null);
  const onSelectRef = useRef(onSelect);
  onSelectRef.current = onSelect;

  // Initialise the map once.
  useEffect(() => {
    if (mapRef.current) return undefined;
    const map = L.map(containerRef.current, { scrollWheelZoom: false }).setView([39.5, -8.0], 5);
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
      attribution: '&copy; OpenStreetMap contributors',
      maxZoom: 19,
    }).addTo(map);
    mapRef.current = map;
    layerRef.current = L.layerGroup().addTo(map);
    return () => {
      map.remove();
      mapRef.current = null;
      layerRef.current = null;
    };
  }, []);

  // Re-plot markers whenever the result set changes.
  useEffect(() => {
    const map = mapRef.current;
    const layer = layerRef.current;
    if (!map || !layer) return;
    layer.clearLayers();
    const points = [];
    (properties || []).forEach((p) => {
      const lat = p.address?.latitude;
      const lng = p.address?.longitude;
      if (typeof lat !== 'number' || typeof lng !== 'number' || (lat === 0 && lng === 0)) return;
      const marker = L.circleMarker([lat, lng], {
        radius: 8, color: '#ff385c', fillColor: '#ff385c', fillOpacity: 0.85, weight: 2,
      });
      marker.bindPopup(`<strong>${escapeHtml(p.title)}</strong><br/>${escapeHtml(p.pricePerNight.display)} / night`);
      marker.on('click', () => onSelectRef.current && onSelectRef.current(p.id));
      marker.addTo(layer);
      points.push([lat, lng]);
    });
    if (points.length > 0) {
      map.fitBounds(points, { padding: [40, 40], maxZoom: 14 });
    }
  }, [properties]);

  return <div ref={containerRef} className="map-view" />;
}
