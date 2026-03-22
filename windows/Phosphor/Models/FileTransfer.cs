using CommunityToolkit.Mvvm.ComponentModel;

namespace Phosphor.Models;

public sealed partial class FileTransfer : ObservableObject
{
    public string Id { get; }
    public string FileName { get; }
    public long Size { get; }

    [ObservableProperty]
    public partial long BytesWritten { get; set; }

    [ObservableProperty]
    public partial string Status { get; set; } = "pending";

    [ObservableProperty]
    public partial string? Error { get; set; }

    public double Progress => Size > 0 ? (double)BytesWritten / Size : 0;

    public FileTransfer(string id, string fileName, long size)
    {
        Id = id;
        FileName = fileName;
        Size = size;
    }

    partial void OnBytesWrittenChanged(long value)
    {
        OnPropertyChanged(nameof(Progress));
    }
}
