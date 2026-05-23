import { NavigationContainer } from '@react-navigation/native';
import { createNativeStackNavigator } from '@react-navigation/native-stack';
import { StatusBar } from 'expo-status-bar';
import { AuthProvider } from './src/auth/AuthContext';
import ExploreScreen from './src/screens/ExploreScreen';
import PropertyScreen from './src/screens/PropertyScreen';
import TripsScreen from './src/screens/TripsScreen';
import { HeaderAuthButton } from './src/screens/HeaderAuthButton';

const Stack = createNativeStackNavigator();

export default function App() {
  return (
    <AuthProvider>
      <StatusBar style="dark" />
      <NavigationContainer>
        <Stack.Navigator
          screenOptions={{
            headerStyle: { backgroundColor: '#fff' },
            headerTitleStyle: { color: '#ff385c', fontWeight: '800' },
            headerRight: () => <HeaderAuthButton />,
          }}
        >
          <Stack.Screen name="Explore" component={ExploreScreen} options={{ title: 'AirHost' }} />
          <Stack.Screen name="Property" component={PropertyScreen} options={{ title: 'Listing' }} />
          <Stack.Screen name="Trips" component={TripsScreen} options={{ title: 'My trips' }} />
        </Stack.Navigator>
      </NavigationContainer>
    </AuthProvider>
  );
}
