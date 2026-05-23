import { Link } from 'react-router-dom';

export default function PropertyCard({ property }) {
  const photo = property.photos?.[0]?.url;
  return (
    <Link to={`/properties/${property.id}`} className="card">
      <div className="card-photo">
        {photo ? <img src={photo} alt={property.title} /> : <div className="card-photo-placeholder">No photo</div>}
      </div>
      <div className="card-body">
        <div className="card-title">{property.title}</div>
        <div className="card-meta">
          {property.address.city}, {property.address.country}
        </div>
        <div className="card-price">
          <strong>{property.pricePerNight.display}</strong> / night
        </div>
      </div>
    </Link>
  );
}
