using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Data;

namespace Phosphor.Helpers;

/// <summary>
/// Converts bool to Visibility. True = Visible, False = Collapsed.
/// </summary>
public sealed partial class BoolToVisibilityConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, string language)
    {
        if (value is bool b)
        {
            // If parameter is "invert", reverse the logic
            if (parameter is string s && s.Equals("invert", StringComparison.OrdinalIgnoreCase))
                return b ? Visibility.Collapsed : Visibility.Visible;

            return b ? Visibility.Visible : Visibility.Collapsed;
        }
        return Visibility.Collapsed;
    }

    public object ConvertBack(object value, Type targetType, object parameter, string language)
    {
        throw new NotImplementedException();
    }
}
