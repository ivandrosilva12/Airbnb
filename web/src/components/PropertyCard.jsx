import { Link } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { useFavorites } from '../context/FavoritesContext';

export default function PropertyCard({ property }) {
  const photo = property.photos?.[0]?.url;
  const { authenticated } = useAuth();
  const { isFavorite, toggle } = useFavorites();
  const saved = isFavorite(property.id);

  function onHeart(e) {
    e.preventDefault();
    e.stopPropagation();
    toggle(property.id);
  }

  return (
    <Link to={`/properties/${property.id}`} className="card">
      <div className="card-photo">
        {photo ? <img src={photo} alt={property.title} /> : <div className="card-photo-placeholder">No photo</div>}
        {authenticated && (
          <button
            type="button"
            className={`heart${saved ? ' heart-on' : ''}`}
            onClick={onHeart}
            aria-label={saved ? 'Remove from saved' : 'Save'}
          >
            {saved ? '♥' : '♡'}
          </button>
        )}
      </div>
      <div className="card-body">
        <div className="card-title-row">
          <span className="card-title">{property.title}</span>
          {property.reviewCount > 0 && (
            <span className="card-rating">★ {property.averageRating.toFixed(1)}</span>
          )}
        </div>
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
